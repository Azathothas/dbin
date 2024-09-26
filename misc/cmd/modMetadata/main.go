package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

type labeledString struct {
	mainURL     string
	fallbackURL string
	label       string
}

type Item struct {
	Name string `json:"name"`
	//	RealName     *string `json:"real_name,omitempty"`
	Description  string `json:"description,omitempty"`
	DownloadURL  string `json:"download_url,omitempty"`
	Size         string `json:"size,omitempty"`
	B3sum        string `json:"b3sum,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
	BuildDate    string `json:"build_date,omitempty"`
	RepoURL      string `json:"repo_url,omitempty"`
	RepoAuthor   string `json:"repo_author,omitempty"`
	RepoInfo     string `json:"repo_info,omitempty"`
	RepoUpdated  string `json:"repo_updated,omitempty"`
	RepoReleased string `json:"repo_released,omitempty"`
	RepoVersion  string `json:"repo_version,omitempty"`
	RepoStars    string `json:"repo_stars,omitempty"`
	RepoLanguage string `json:"repo_language,omitempty"`
	RepoLicense  string `json:"repo_license,omitempty"`
	RepoTopics   string `json:"repo_topics,omitempty"`
	WebURL       string `json:"web_url,omitempty"`
	ExtraBins    string `json:"extra_bins,omitempty"`
}

func urldecode(encoded string) (string, error) {
	return url.PathUnescape(encoded)
}

func processItems(items []Item, arch string, repo_label string) []Item {
	for i, item := range items {
		// Parse the download URL to get its path
		parsedURL, err := url.Parse(item.DownloadURL)
		if err != nil {
			// Handle the error appropriately
			continue
		}

		// Extract the path from the URL and remove leading slashes
		cleanPath := parsedURL.Path
		if strings.HasPrefix(cleanPath, "/") {
			cleanPath = cleanPath[1:]
		}

		// Remove the architecture-specific path from the download URL path
		if strings.HasPrefix(cleanPath, arch+"/") {
			cleanPath = strings.TrimPrefix(cleanPath, arch+"/")
		}

		// Remove the repo's label
		if strings.HasPrefix(cleanPath, repo_label+"/") {
			cleanPath = strings.TrimPrefix(cleanPath, repo_label+"/")
		}

		// Ensure real_name is always set
		items[i].Name = cleanPath
	}
	return items
}

func downloadJSON(url string) ([]Item, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var items []Item
	err = json.Unmarshal(body, &items)
	if err != nil {
		return nil, err
	}

	return items, nil
}

func saveJSON(filename string, items []Item) error {
	jsonData, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return err
	}

	return nil
}

// downloadWithFallback tries to download from the primary URL and falls back if it fails
func downloadWithFallback(repo labeledString) ([]Item, error) {
	// Try main URL first
	items, err := downloadJSON(repo.mainURL)
	if err == nil {
		fmt.Printf("Downloaded JSON from: %s\n", repo.mainURL)
		return items, nil
	}

	// If main URL fails, try the fallback URL
	fmt.Printf("Error downloading from main URL %s: %v. Trying fallback URL...\n", repo.mainURL, err)
	items, err = downloadJSON(repo.fallbackURL)
	if err == nil {
		fmt.Printf("Downloaded JSON from fallback URL: %s\n", repo.fallbackURL)
		return items, nil
	}

	// Return the error if both fail
	fmt.Printf("Error downloading from fallback URL %s: %v\n", repo.fallbackURL, err)
	return nil, err
}

func main() {
	validatedArchs := []string{"x86_64_Linux", "aarch64_arm64_Linux", "arm64_v8a_Android", "x64_Windows"}

	for _, arch := range validatedArchs {
		repos := []labeledString{
			// Main URL and fallback URL are added for each repo
			{"https://bin.ajam.dev/" + arch + "/METADATA.json",
				"https://huggingface.co/datasets/Azathothas/Toolpacks-Snapshots/resolve/main/" + arch + "/METADATA.json?download=true",
				"Toolpacks"},
			{"https://bin.ajam.dev/" + arch + "/Baseutils/METADATA.json",
				"https://huggingface.co/datasets/Azathothas/Toolpacks-Snapshots/resolve/main/Baseutils/METADATA.json?download=true",
				"Baseutils"},
			// --- Toolpacks-extra
				{"https://huggingface.co/datasets/Azathothas/Toolpacks-Extras/resolve/main/" + arch + "/METADATA.json",
				"https://huggingface.co/datasets/Azathothas/Toolpacks-Extras/resolve/main/",
				"Toolpacks-extras"},
		}

		for _, repo := range repos {
			items, err := downloadWithFallback(repo)
			if err != nil {
				fmt.Printf("Error downloading JSON from both main and fallback URLs for repo %s: %v\n", repo.label, err)
				continue
			}

			processedItems := processItems(items, arch, repo.label)

			outputFile := fmt.Sprintf("%s.dbin_%s.json", repo.label, arch)

			if err := saveJSON(outputFile, processedItems); err != nil {
				fmt.Printf("Error saving JSON to %s: %v\n", outputFile, err)
				continue
			}
			fmt.Printf("Processed and saved to %s\n", outputFile)
		}
	}
}
