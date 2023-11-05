package main

import (
  "errors"
	"fmt"
	"log"
	// "net/url"
	"os" // https://pkg.go.dev/os
	"strconv"
	"strings"
	"time"

	"github.com/arran4/golang-ical"
	"github.com/cavaliergopher/grab/v3" // https://pkg.go.dev/github.com/cavaliergopher/grab/v3
	"github.com/go-rod/rod"             // https://pkg.go.dev/github.com/go-rod/rod
	// "github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
)

// Just using /tmp because that is simplest
const tmpDir = "/tmp/movie-cal-20231026"
const downloadDir = tmpDir + "/downloads"
const cacheDir = tmpDir + "/cache"

func ensureDirs() {
	err := os.MkdirAll(downloadDir, 0750)
	if err != nil {
		log.Fatal(err)
	}
	err = os.MkdirAll(cacheDir, 0750)
	if err != nil {
		log.Fatal(err)
	}
}

func putFileCache(cacheFileName string, path string) (string, error) {
	cachePath := cacheDir + "/" + cacheFileName
	err := os.Rename(path, cachePath)
	if err != nil {
		return "", err
	}
	return cachePath, nil
}

func getFileCache(cacheFileName string) (string, error) {
	cachePath := cacheDir + "/" + cacheFileName
	_, err := os.Lstat(cachePath)
	if err != nil {
		return "", err
	}
	return cachePath, nil
}

func downloadFile(filename string, url string) (string, error) {
	cachePath, err := getFileCache(filename)
	if err == nil {
		log.Printf("Using cached file: %s", cachePath)
		return cachePath, nil
	}
	client := grab.NewClient()
	req, _ := grab.NewRequest(downloadDir, url)

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

	cachePath, err = putFileCache(filename, resp.Filename)
	if err != nil {
		return "", nil
	}
	return cachePath, nil
}

func openIcsFile(s string) (*ics.Calendar, error) {
	fileReader, err := os.Open(s)
	if err != nil {
		return nil, err
	}
	return ics.ParseCalendar(fileReader)
}

type Screening struct {
	title   string
	theater string
	time    time.Time
	url     string // url.URL
	// year string
}

func printScreenings(screenings []Screening) {
	tz, err := time.LoadLocation("Local")
	if err != nil {
		tz, err = time.LoadLocation("America/Los_Angeles")
		if err != nil {
			log.Fatal("Timezone error")
		}
	}
	println("SCREENINGS:\n============================================================")
	for _, s := range screenings {
		println("TITLE: " + s.title)
		println("TIME: " + s.time.In(tz).Format("Mon Jan _2 3:00 PM MST 2006"))
		println("THEATER: " + s.theater)
		println("URL: " + s.url)
		println()
	}
}

func getClintonStateTheaterScreenings() []Screening {
	filename, err := downloadFile("cstpdx.ics", "https://cstpdx.com/schedule/list/?ical=1")
	if err != nil {
		log.Fatal(err)
	}
	cal, err := openIcsFile(filename)
	// cal.SerializeTo(os.Stdout)
	screenings := make([]Screening, 0, 100)
	for _, e := range cal.Events() {
		t, err := e.GetStartAt()
		summary := e.GetProperty("SUMMARY")
		url := e.GetProperty("URL")
		if err != nil {
			continue
		}
		s := Screening{title: summary.Value, time: t, theater: "Clinton State Theater", url: url.Value}
		screenings = append(screenings, s)
	}
	return screenings
}

func getBrowser() *rod.Browser {
	chromeBin, ok := os.LookupEnv("CHROME_BIN")
	if !ok {
		chromeBin, ok = launcher.LookPath()
		if !ok {
			log.Fatal("Cannot find chrome executable")
		}
	}
	url := launcher.New().Bin(chromeBin).MustLaunch()
	return rod.New().ControlURL(url).MustConnect()
}

func getHollywoodTheaterScreenings(browser *rod.Browser) []Screening {
	screenings := make([]Screening, 0, 100)
	page := browser.MustPage("https://hollywoodtheatre.org/").MustWaitStable()
	nowShowingEvents := page.MustElements(".event-grid-item")
	for i, eventEl := range nowShowingEvents {
		log.Printf("Event #%d", i+1)
		titleEl, err := eventEl.Element(".event-grid-header h3")
		if err != nil {
			log.Printf("Cannot find title")
			continue
		}
		title, err := titleEl.Text()
		if err != nil {
			log.Printf("Cannot find title")
			continue
		}
		log.Printf("Title: %s", title)
		dayEl, err := eventEl.Element("div.event-grid-showtimes div.carousel-item.active h4.showtimes_date_header")
		if err != nil {
			log.Printf("Cannot find day")
			continue
		}
		day, err := dayEl.Text()
		if err != nil {
			log.Printf("Cannot find day")
			continue
		}
		log.Printf("Day: %s", day)
		dayFields := strings.Fields(day)
		if len(dayFields) != 3 {
			log.Printf("Day is unrecognizable: %s", day)
		}
		month := dayFields[1]
		dayNum := dayFields[2]
		times, err := eventEl.Elements("div.event-grid-showtimes div.carousel-item.active .showtime-square a")
		if err != nil {
			log.Printf("Cannot find times")
			continue
		}
		for _, timeEl := range times {
			rawTime, err := timeEl.Text()
			if err != nil {
				log.Printf("Cannot find time")
				continue
			}
			timeFields := strings.Fields(rawTime)
			if len(timeFields) != 2 {
				log.Printf("Time is unrecognizable: %s", rawTime)
				continue
			}
			timeNums := timeFields[0]
			timeAmPm := strings.ToUpper(timeFields[1])
			year := strconv.Itoa(time.Now().Year())
			// timeZone := "-0700"
			location, err := time.LoadLocation("America/Los_Angeles")
			if err != nil {
				log.Printf("Could not load theater time zone location")
				continue
			}
			normalizedTime := strings.Join([]string{timeNums, timeAmPm, month, dayNum, year}, " ")
			result, err := time.ParseInLocation("3:04 PM January _2 2006", normalizedTime, location)
			if err != nil {
				log.Printf("Cannot parse time: %s", normalizedTime)
				var parseErr *time.ParseError
				if errors.As(err, &parseErr) {
					log.Print(parseErr)
				}
				continue
			}
			url := timeEl.MustAttribute("href")
			s := Screening{
				title:   title,
				time:    result,
				theater: "Hollywood Theater",
				url:     "https://hollywoodtheatre.org" + *url,
			}
			screenings = append(screenings, s)
		}
	}
	return screenings
}

func main() {
	fmt.Printf("Starting movie-cal...\n")
	ensureDirs()
	browser := getBrowser()
	defer browser.MustClose()
	// printScreenings(getClintonStateTheaterScreenings())
	printScreenings(getHollywoodTheaterScreenings(browser))
}
