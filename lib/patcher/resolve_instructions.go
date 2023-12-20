package patcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type ResolvedInstructions struct {
	Instructions []Instruction
	BaseUrl      *url.URL
	VersionName  string
}

// productsJson contains the relevant parts of the products.json file.
type productsJson struct {
	Games []struct {
		ReleaseUrl string `json:"legacy_data_path"`
		Tag        string `json:"tag"`
	} `json:"games"`
}

// releaseJson contains the relevant parts of the release.json file.
type releaseJson struct {
	Game struct {
		InstructionsHash string `json:"instructions_hash"`
		PatchPath        string `json:"patch_path"`
		Mirrors          []struct {
			Url string `json:"url"`
		} `json:"mirrors"`
		VersionName string `json:"version_name"`
	} `json:"game"`
}

// ResolveInstructions finds the instructions and URL containing the patch files by looking up a product
// through the root products.json file.
func ResolveInstructions(productsUrl *url.URL, product string) (*ResolvedInstructions, error) {
	products, err := fetchJson[productsJson]("products.json", productsUrl)
	if err != nil {
		return nil, err
	}

	var releaseUrl *url.URL
	for _, game := range products.Games {
		if strings.EqualFold(game.Tag, product) {
			releaseUrl, err = url.Parse(game.ReleaseUrl)
			if err != nil {
				return nil, fmt.Errorf("can't convert %q in '%s' to URL: %w", game.ReleaseUrl, productsUrl, err)
			}
		}
	}
	if releaseUrl == nil {
		return nil, fmt.Errorf("couldn't find game '%s' in '%s'", product, productsUrl)
	}

	release, err := fetchJson[releaseJson]("release.json", releaseUrl)
	if err != nil {
		return nil, err
	}

	if len(release.Game.Mirrors) == 0 {
		return nil, fmt.Errorf("there are no mirrors for gmae '%s' in '%s'", product, releaseUrl)
	}

	// Just fetch the first, there's only one mirror nowadays.
	mirrorUrl, err := url.Parse(release.Game.Mirrors[0].Url)
	if err != nil {
		return nil, fmt.Errorf("can't convert %q in '%s' to URL: %w", release.Game.Mirrors[0].Url, releaseUrl, err)
	}
	baseUrl := mirrorUrl.JoinPath(release.Game.PatchPath)
	instructionsUrl := baseUrl.JoinPath("instructions.json")

	instructionsData, err := fetchBytes("instructions.json", instructionsUrl)
	if err != nil {
		return nil, err
	}

	checksum := HashBytes(instructionsData)
	if !strings.EqualFold(release.Game.InstructionsHash, checksum) {
		return nil, fmt.Errorf("'%s' hash mismatch, expected %s got %s", instructionsUrl,
			strings.ToUpper(release.Game.InstructionsHash), strings.ToUpper(checksum))
	}

	instructions, err := DecodeInstructions(instructionsData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode instructions from '%s': %w", instructionsUrl, err)
	}

	return &ResolvedInstructions{
		BaseUrl:      baseUrl,
		Instructions: instructions,
		VersionName:  release.Game.VersionName,
	}, nil
}

func fetchBytes(what string, location *url.URL) ([]byte, error) {
	resp, err := http.Get(location.String())
	if err != nil {
		// Error message very likely contains URL already.
		return nil, fmt.Errorf("failed to fetch %s: %v", what, err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch %s (status %d)", what, resp.StatusCode)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from '%s': %w", location, err)
	}
	return data, nil
}

func fetchJson[T any](what string, location *url.URL) (T, error) {
	var val T
	data, err := fetchBytes(what, location)
	if err != nil {
		return val, err
	}
	if err := json.Unmarshal(data, &val); err != nil {
		return val, fmt.Errorf("failed to decode response from '%s': %w", location, err)
	}
	return val, nil
}
