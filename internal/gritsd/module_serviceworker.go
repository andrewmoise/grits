package gritsd

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"regexp"
	"strings"
)

type ServiceWorkerModuleConfig struct {
	// No options
}

type ServiceWorkerModule struct {
	Config *ServiceWorkerModuleConfig
	Server *Server
}

func NewServiceWorkerModule(server *Server, config *ServiceWorkerModuleConfig) (*ServiceWorkerModule, error) {
	swm := &ServiceWorkerModule{
		Config: config,
		Server: server,
	}

	server.AddModuleHook(func(module Module) {
		httpModule, ok := module.(*HTTPModule)
		if !ok {
			return
		}

		serve := func(path, volumePath string, tmpl bool) {
			httpModule.Mux.HandleFunc(path, httpModule.requestMiddleware(
				swm.serveFromClientVolume(volumePath, tmpl)))
		}

		// JS bundles that get the module/SW template substitution.
		serve("/grits-serviceworker.js", "serviceworker/grits-serviceworker.js", true)
		serve("/grits-GritsClient-sw.js", "lib/grits/GritsClient.js", true)
		serve("/grits-MirrorManager-sw.js", "lib/grits/MirrorManager.js", true)
		serve("/grits-HashVerifier-sw.js", "lib/grits/HashVerifier.js", true)
		serve("/grits-PerformanceTracker-sw.js", "lib/grits/PerformanceTracker.js", true)

		// Self-test HTML — served verbatim from the client volume, no templating.
		serve("/grits/v1/swtest", "serviceworker/swtest.html", false)
		serve("/grits/v1/swtest/", "serviceworker/swtest.html", false)

		// Self-test ping endpoint. The SW intercepts this when active;
		// when absent, the server responds 404 so the test can detect
		// "SW not currently controlling".
		httpModule.Mux.HandleFunc("/grits/v1/swtest/ping", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"sw-not-active"}`, http.StatusNotFound)
		})

		// Explicit interstitial endpoint. The SW redirects here with ?target=
		// when it detects its own hash is stale on a navigation.
		httpModule.Mux.HandleFunc("/grits/v1/sw-interstitial",
			httpModule.requestMiddleware(swm.serveExplicitInterstitial))

		httpModule.WrapContentHandler(func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				sentinel := r.Header.Get("X-Grits-SW-Sentinel")
				bypassCookie, bypassErr := r.Cookie("grits-sw-bypass")
				loadingCookie, loadingErr := r.Cookie("grits-sw-loading")
				fetchMode := r.Header.Get("Sec-Fetch-Mode")

				log.Printf("[SW] content handler: path=%s method=%s fetch-mode=%s sentinel=%q bypass-cookie=%v loading-cookie=%v",
					r.URL.Path, r.Method, fetchMode,
					sentinel,
					bypassErr == nil,
					loadingErr == nil,
				)
				_ = bypassCookie
				_ = loadingCookie

				if sentinel != "" {
					log.Printf("[SW] → sentinel present, serving normally")
					next(w, r)
					return
				}
				if bypassErr == nil {
					log.Printf("[SW] → bypass cookie present, serving normally")
					next(w, r)
					return
				}
				if fetchMode == "navigate" {
					if loadingErr != nil {
						log.Printf("[SW] → navigate without SW and no cooldown cookie, serving interstitial")
						swm.serveInterstitial(w, r)
						return
					}
					log.Printf("[SW] → navigate but cooldown cookie present, serving normally")
				} else {
					log.Printf("[SW] → non-navigate request without sentinel, serving normally")
				}
				next(w, r)
			}
		})
	})

	return swm, nil
}

func (swm *ServiceWorkerModule) clientVolume() Volume {
	return swm.Server.FindVolumeByName("client")
}

func (swm *ServiceWorkerModule) serveFromClientVolume(volumePath string, applyTemplate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vol := swm.clientVolume()
		if vol == nil {
			http.Error(w, "Client volume not found", http.StatusInternalServerError)
			return
		}

		fileNode, err := vol.LookupNode(volumePath, grits.BackendPrincipal)
		if err != nil || fileNode == nil {
			http.Error(w, "File not found", http.StatusInternalServerError)
			return
		}
		defer fileNode.Release()

		blob, err := fileNode.ExportedBlob()
		if err != nil {
			http.Error(w, "Error loading file", http.StatusInternalServerError)
			return
		}

		data, err := blob.Read(0, blob.GetSize())
		if err != nil {
			http.Error(w, "Error reading file", http.StatusInternalServerError)
			return
		}

		result := string(data)
		if applyTemplate {
			result = processTemplateForSW(result)
			result = strings.ReplaceAll(result, "{{SW_DIR_HASH}}", string(swm.getClientDirHash()))

			swNode, err := vol.LookupNode("serviceworker/grits-serviceworker.js", grits.BackendPrincipal)
			if err != nil || swNode == nil {
				http.Error(w, "Service worker script not found", http.StatusInternalServerError)
				return
			}
			defer swNode.Release()
			result = strings.ReplaceAll(result, "{{SW_SCRIPT_HASH}}", string(swNode.Metadata().ContentHash))
		}

		w.Header().Set("Content-Type", contentTypeForVolumePath(volumePath))
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, result)
	}
}

// contentTypeForVolumePath returns an appropriate Content-Type for files
// served through serveFromClientVolume. Kept small — we only serve a handful
// of file types through this path.
func contentTypeForVolumePath(volumePath string) string {
	switch {
	case strings.HasSuffix(volumePath, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(volumePath, ".css"):
		return "text/css"
	case strings.HasSuffix(volumePath, ".json"):
		return "application/json"
	default:
		return "application/javascript"
	}
}

func (swm *ServiceWorkerModule) getClientDirHash() grits.BlobAddr {
	vol := swm.clientVolume()
	if vol == nil {
		return "(no client volume)"
	}
	node, err := vol.LookupNode("serviceworker", grits.BackendPrincipal)
	if err != nil || node == nil {
		return grits.BlobAddr(fmt.Sprintf("(error: %v)", err))
	}
	defer node.Release()
	return node.Metadata().ContentHash
}

// processTemplateForSW converts a shared JS file into its service worker variant
// by toggling the %FOR MODULE% / %FOR SERVICEWORKER% comment markers.
func processTemplateForSW(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	commentRe := regexp.MustCompile(`^\s*//`)
	uncommentRe := regexp.MustCompile(`^(\s*)//`)

	for _, line := range lines {
		switch {
		case strings.Contains(line, "%FOR MODULE%"):
			// Comment out module-only lines in the SW version.
			if commentRe.MatchString(line) {
				out = append(out, line)
			} else {
				out = append(out, "// "+line)
			}
		case strings.Contains(line, "%FOR SERVICEWORKER%"):
			// Uncomment SW-only lines.
			if commentRe.MatchString(line) {
				out = append(out, uncommentRe.ReplaceAllString(line, "$1"))
			} else {
				out = append(out, line)
			}
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// serveInterstitial is the content-handler fallback path: a navigation
// arrived without a sentinel header and without a bypass cookie, so we
// assume no SW is installed and inject one via the interstitial, bouncing
// the user back to whatever URL they originally requested.
func (swm *ServiceWorkerModule) serveInterstitial(w http.ResponseWriter, r *http.Request) {
	swm.writeInterstitialHTML(w, r.URL.RequestURI())
}

// serveExplicitInterstitial is reached when the SW itself redirects here
// after detecting a stale hash on a navigate event. The target URL is
// carried in the ?target= query parameter.
func (swm *ServiceWorkerModule) serveExplicitInterstitial(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	target = sanitizeInterstitialTarget(target)
	swm.writeInterstitialHTML(w, target)
}

// sanitizeInterstitialTarget enforces that the target is a same-origin
// relative path, guarding against open-redirect abuse. Rejects anything
// that looks like a scheme or protocol-relative URL and ensures a leading
// slash. Defaults to "/" on anything suspect or missing.
func sanitizeInterstitialTarget(target string) string {
	if target == "" {
		return "/"
	}
	if strings.HasPrefix(target, "//") || strings.Contains(target, "://") {
		return "/"
	}
	if !strings.HasPrefix(target, "/") {
		return "/" + target
	}
	return target
}

// writeInterstitialHTML emits the registration-and-redirect HTML page.
// Shared between serveInterstitial (content-handler fallback) and
// serveExplicitInterstitial (SW-redirected) so there's exactly one place
// the registration dance lives.
func (swm *ServiceWorkerModule) writeInterstitialHTML(w http.ResponseWriter, target string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{
		Name:     "grits-sw-loading",
		Value:    "1",
		Path:     "/",
		MaxAge:   30,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Loading…</title></head>
<body>
<script>
(function() {
  var target = %q;

  // Listen for SW telling us to delete the cooldown cookie (hash mismatch unregister)
  navigator.serviceWorker.addEventListener('message', function(event) {
    if (event.data?.type === 'DELETE_COOLDOWN_COOKIE') {
      document.cookie = 'grits-sw-loading=; path=/; max-age=0; SameSite=Lax';
    }
  });

  if (!('serviceWorker' in navigator)) {
    document.cookie = 'grits-sw-bypass=1; path=/; max-age=28800; SameSite=Lax';
    document.cookie = 'grits-sw-loading=; path=/; max-age=0; SameSite=Lax';
    window.location.replace(target);
    return;
  }

  // Unregister any existing SW first to ensure clean state
  navigator.serviceWorker.getRegistrations().then(function(registrations) {
    return Promise.all(registrations.map(r => r.unregister()));
  }).then(function() {
    return navigator.serviceWorker.register('/grits-serviceworker.js');
  }).then(function(reg) {
    // Delete cooldown cookie on successful registration
    document.cookie = 'grits-sw-loading=; path=/; max-age=0; SameSite=Lax';

    function proceed() {
      window.location.replace(target);
    }

    if (navigator.serviceWorker.controller) {
      proceed();
      return;
    }
    navigator.serviceWorker.addEventListener('controllerchange', proceed);
    var sw = reg.installing || reg.waiting || reg.active;
    if (sw) {
      sw.addEventListener('statechange', function() {
        if (this.state === 'activated') proceed();
      });
    }
  }).catch(function(err) {
    console.error('[Grits] SW registration failed:', err);
    document.cookie = 'grits-sw-bypass=1; path=/; max-age=28800; SameSite=Lax';
    document.cookie = 'grits-sw-loading=; path=/; max-age=0; SameSite=Lax';
    window.location.replace(target);
  });
})();
</script>
</body>
</html>`, target)
}

func (swm *ServiceWorkerModule) Start() error { return nil }
func (swm *ServiceWorkerModule) Stop() error  { return nil }

func (swm *ServiceWorkerModule) GetModuleName() string { return "serviceworker" }
func (*ServiceWorkerModule) GetDependencies() []*Dependency {
	return []*Dependency{}
}
func (swm *ServiceWorkerModule) GetConfig() any { return swm.Config }
