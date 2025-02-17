package main

import (
	"fmt"
	"path/filepath"
)

type BinaryInfo struct {
	Name        string   `json:"pkg"`
	PrettyName  string   `json:"pkg_name"`
	PkgId       string   `json:"pkg_id"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	DownloadURL string   `json:"download_url,omitempty"`
	Size        string   `json:"size,omitempty"`
	Bsum        string   `json:"bsum,omitempty"`
	Shasum      string   `json:"shasum,omitempty"`
	BuildDate   string   `json:"build_date,omitempty"`
	BuildScript string   `json:"build_script,omitempty"`
	BuildLog    string   `json:"build_log,omitempty"`
	Categories  string   `json:"categories,omitempty"`
	ExtraBins   string   `json:"provides,omitempty"`
	GhcrBlob    string   `json:"ghcr_blob,omitempty"`
	Rank        uint16   `json:"rank,omitempty"`
	Notes       []string `json:"notes,omitempty"`
	SrcURLs     []string `json:"src_urls,omitempty"`
	WebURLs     []string `json:"web_urls,omitempty"`
}

func findBinaryInfo(bEntry binaryEntry, metadata map[string]interface{}) (BinaryInfo, bool) {
	matchingBins, highestRank := findMatchingBins(bEntry, metadata)

	if len(matchingBins) == 0 {
		return BinaryInfo{}, false
	}

	selectedBin := selectHighestRankedBin(matchingBins, highestRank)

	return populateBinaryInfo(selectedBin), true
}

func populateBinaryInfo(binMap map[string]interface{}) BinaryInfo {
	getString := func(key string) string {
		if val, ok := binMap[key].(string); ok {
			return val
		}
		return ""
	}

	getStringSlice := func(key string) []string {
		if val, ok := binMap[key]; ok {
			switch v := val.(type) {
			case []interface{}:
				strSlice := make([]string, len(v))
				for i, item := range v {
					if str, ok := item.(string); ok {
						strSlice[i] = str
					}
				}
				return strSlice
			}
		}
		return []string{}
	}

	getUint16 := func(key string) uint16 {
		if val, ok := binMap[key]; ok {
			return uint16(val.(float64))
		}
		return 0
	}

	return BinaryInfo{
		Name:        getString("pkg"),
		PrettyName:  getString("pkg_name"),
		PkgId:       getString("pkg_id"),
		Description: getString("description"),
		Version:     getString("version"),
		DownloadURL: getString("download_url"),
		Size:        getString("size"),
		Bsum:        getString("bsum"),
		Shasum:      getString("shasum"),
		BuildDate:   getString("build_date"),
		BuildScript: getString("build_script"),
		BuildLog:    getString("build_log"),
		Categories:  getString("categories"),
		ExtraBins:   getString("provides"),
		GhcrBlob:    getString("ghcr_blob"),
		SrcURLs:     getStringSlice("src_urls"),
		WebURLs:     getStringSlice("web_urls"),
		Notes:       getStringSlice("notes"),
		Rank:        getUint16("rank"),
	}
}

func getBinaryInfo(config *Config, bEntry binaryEntry, metadata map[string]interface{}) (*BinaryInfo, error) {
	// Check if the package is installed, prioritize info of a installed version.
	if instBEntry := bEntryOfinstalledBinary(filepath.Join(config.InstallDir, bEntry.Name)); bEntry.PkgId == "" && instBEntry.PkgId != "" {
		bEntry = instBEntry
	}

	binInfo, found := findBinaryInfo(bEntry, metadata)
	if found {
		return &binInfo, nil
	}

	return nil, fmt.Errorf("error: info for the requested binary ('%s') not found in any of the metadata files", parseBinaryEntry(bEntry, false))
}
