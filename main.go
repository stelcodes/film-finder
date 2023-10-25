package main

import (
	"fmt"
	"github.com/cavaliergopher/grab/v3" // https://pkg.go.dev/github.com/cavaliergopher/grab/v3
	"log"
	"os" // https://pkg.go.dev/os
	"time"
)

// Just using /tmp because that is simplest
const CACHE_DIR = "/tmp/movie-cal-cache"

func downloadFile(s string) (string, error) {
	client := grab.NewClient()
	req, _ := grab.NewRequest(CACHE_DIR, s)

	// start download
	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	fmt.Printf("  %v\n", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		return "", err
	}

	fmt.Printf("Download saved to %v\n", resp.Filename)
  return resp.Filename, nil
}

func main() {
	fmt.Printf("Starting movie-cal...\n")
  err := os.MkdirAll(CACHE_DIR, 0750)
	if err != nil {
		log.Fatal(err)
	}
  _, err = downloadFile("https://cstpdx.com/schedule/list/?ical=1")
	if err != nil {
		log.Fatal(err)
	}

}
