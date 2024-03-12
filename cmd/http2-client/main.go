package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	// Define a flag for the server address
	serverAddress := flag.String("h", "localhost:1787", "Server address with optional port")

	// Parse the flags
	flag.Parse()

	// After parsing, flag.Args() contains the non-flag command-line arguments
	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("Usage: client [options] <command> [arguments]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	command := args[0] // The first non-flag argument is the command

	switch command {
	case "get":
		if len(os.Args) != 4 {
			fmt.Println("Usage: client get <remote-name> <local-name>")
			os.Exit(1)
		}
		getFile(*serverAddress, os.Args[2], os.Args[3])
	case "put":
		recursive := false
		startIndex := 2
		if os.Args[2] == "-r" {
			recursive = true
			startIndex++
		}
		if len(os.Args) < startIndex+2 {
			fmt.Println("Usage: client put [-r] <local-name> <remote-name>")
			os.Exit(1)
		}
		localName := os.Args[startIndex]
		remoteName := os.Args[startIndex+1]
		if recursive {
			err := putDirectoryRecursively(*serverAddress, localName, remoteName)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}
		} else {
			err := putFile(*serverAddress, localName, remoteName)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}
		}
	case "rm":
		if len(os.Args) < 3 {
			fmt.Println("Usage: client rm <remote-name> ...")
			os.Exit(1)
		}
		err := removeFiles(*serverAddress, os.Args[2:])
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
	case "ls":
		err := listFiles(*serverAddress)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
	default:
		fmt.Println("Unknown command:", command)
		os.Exit(1)
	}
}

func getFile(serverAddress, remoteName, localName string) error {
	resp, err := http.Get("http://" + serverAddress + "/grits/v1/content/root/" + remoteName)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Got status %d: %s", resp.StatusCode, string(respBody))
	}

	localFile, err := os.Create(localName)
	if err != nil {
		return err
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func putFile(serverAddress, localName, remoteName string) error {
	file, err := os.Open(localName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a new PUT request
	req, err := http.NewRequest(http.MethodPut, "http://"+serverAddress+"/grits/v1/content/root/"+remoteName, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	// Send the request using an http.Client
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Got status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func putDirectoryRecursively(serverAddress, localDir, remoteDir string) error {
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			localPath := path
			remotePath := filepath.Join(remoteDir, path[len(localDir):])
			err := putFile(serverAddress, localPath, remotePath)
			if err != nil {
				return err
			}
		}
		return nil
	})

	return err
}

func removeFiles(serverAddress string, remoteNames []string) error {
	for _, remoteName := range remoteNames {
		req, err := http.NewRequest(http.MethodDelete, "http://"+serverAddress+"/grits/v1/content/root/"+remoteName, nil)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("Got status %d: %s", resp.StatusCode, string(respBody))
		}
	}

	return nil
}

func listFiles(serverAddress string) error {
	resp, err := http.Get("http://" + serverAddress + "/grits/v1/tree")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Got status %d: %s", resp.StatusCode, string(respBody))
	}

	// Read and parse the JSON response
	var files map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return err
	}

	for name, hash := range files {
		fmt.Printf("%s -> %s\n", name, hash)
	}

	return nil
}
