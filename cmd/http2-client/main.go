package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	var command string
	if len(os.Args) < 2 {
		fmt.Println("Usage: client <command> [arguments]")
		os.Exit(1)
	}
	command = os.Args[1]

	switch command {
	case "get":
		if len(os.Args) != 4 {
			fmt.Println("Usage: client get <remote-name> <local-name>")
			os.Exit(1)
		}
		getFile(os.Args[2], os.Args[3])
	case "put":
		if len(os.Args) != 4 {
			fmt.Println("Usage: client put <local-name> <remote-name>")
			os.Exit(1)
		}
		putFile(os.Args[2], os.Args[3])
	case "rm":
		if len(os.Args) < 3 {
			fmt.Println("Usage: client rm <remote-name> ...")
			os.Exit(1)
		}
		removeFiles(os.Args[2:])
	case "ls":
		listFiles()
	default:
		fmt.Println("Unknown command:", command)
		os.Exit(1)
	}
}

func getFile(remoteName, localName string) {
	resp, err := http.Get("http://localhost:1787/grits/v1/namespace/" + remoteName)
	if err != nil {
		fmt.Println("Error getting file:", err)
		return
	}
	defer resp.Body.Close()

	localFile, err := os.Create(localName)
	if err != nil {
		fmt.Println("Error creating local file:", err)
		return
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, resp.Body)
	if err != nil {
		fmt.Println("Error saving file:", err)
	}
}

func putFile(localName, remoteName string) {
	file, err := os.Open(localName)
	if err != nil {
		fmt.Println("Error opening local file:", err)
		return
	}
	defer file.Close()

	resp, err := http.Post("http://localhost:1787/grits/v1/namespace/"+remoteName, "application/octet-stream", file)
	if err != nil {
		fmt.Println("Error uploading file:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("File uploaded successfully")
}

func removeFiles(remoteNames []string) {
	for _, remoteName := range remoteNames {
		req, err := http.NewRequest(http.MethodDelete, "http://localhost:1787/grits/v1/namespace/"+remoteName, nil)
		if err != nil {
			fmt.Println("Error creating request:", err)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Println("Error deleting file:", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fmt.Println("File removed:", remoteName)
		} else {
			fmt.Println("Failed to remove file:", remoteName)
		}
	}
}

func listFiles() {
	resp, err := http.Get("http://localhost:1787/grits/v1/root/root")
	if err != nil {
		fmt.Println("Error fetching root namespace:", err)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading root namespace response:", err)
		return
	}
	rootHash := string(bodyBytes)

	listResp, err := http.Get("http://localhost:1787/grits/v1/sha256/" + rootHash)
	if err != nil {
		fmt.Println("Error listing files:", err)
		return
	}
	defer listResp.Body.Close()

	var files map[string]string
	if err := json.NewDecoder(listResp.Body).Decode(&files); err != nil {
		fmt.Println("Error decoding file list:", err)
		return
	}

	for name, hash := range files {
		fmt.Printf("%s -> %s\n", name, hash)
	}
}
