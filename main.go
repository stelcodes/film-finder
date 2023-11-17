package main

import (
	"errors"
	"fmt"
	"log"
	// "net/url"
	"os" // https://pkg.go.dev/os
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arran4/golang-ical"
	"github.com/cavaliergopher/grab/v3" // https://pkg.go.dev/github.com/cavaliergopher/grab/v3
	"github.com/go-rod/rod"             // https://pkg.go.dev/github.com/go-rod/rod
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
	fileInfo, err := os.Lstat(cachePath)
	if err != nil {
		return "", err
	}
	cacheExpired := time.Now().Sub(fileInfo.ModTime()) > 24*time.Hour
	if cacheExpired {
		return "", errors.New("File in cache has expired")

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

func loadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		log.Fatalf("Could not load time location: %s", name)
	}
	return location
}

func getTime(format string, datetime string, location *time.Location) (time.Time, error) {
	result, err := time.ParseInLocation(format, datetime, location)
	if err != nil {
		log.Printf("Cannot parse time: %s", datetime)
		var parseErr *time.ParseError
		if errors.As(err, &parseErr) {
			log.Print(parseErr)
		}
	}
	return result, err
}

var locations = map[string]*time.Location{
	"Portland": loadLocation("America/Los_Angeles"),
}

var shortWeekdays = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

var thisYear = time.Now().Year()

func scrapeClintonStateTheater() []Screening {
	log.Printf("Scraping Clinton State Theater...")
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

func scrapeEventGrid(eventGridItemEls rod.Elements) []Screening {
	screenings := []Screening{}
	for i, eventGridItemEl := range eventGridItemEls {
		log.Printf("Event #%d", i+1)
		titleEl, err := eventGridItemEl.Element(".event-grid-header h3")
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
		dayEl, err := eventGridItemEl.Element("div.event-grid-showtimes div.carousel-item.active h4.showtimes_date_header")
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
		times, err := eventGridItemEl.Elements("div.event-grid-showtimes div.carousel-item.active .showtime-square a")
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

func scrapeHollywoodTheater(browser *rod.Browser) []Screening {
	log.Printf("Scraping Hollywood Theater...")
	screenings := []Screening{}
	page := browser.MustPage("https://hollywoodtheatre.org/").MustWaitStable()
	defer page.MustClose()
	eventGridItemEls := page.MustElements(".event-grid-item")
	screenings = append(screenings, scrapeEventGrid(eventGridItemEls)...)
	buttonEl, err := page.Element("a[data-events-target=\"comingSoonTab\"]")
	if err != nil {
		log.Printf("Cannot click \"Coming Soon\" button")
		return screenings
	}
	buttonEl.MustClick().WaitStable(time.Second * 3)
	eventGridItemEls = page.MustElements(".event-grid-item")
	screenings = append(screenings, scrapeEventGrid(eventGridItemEls)...)
	return screenings
}

func scrapeAcademyTheater(browser *rod.Browser) []Screening {
	log.Printf("Scraping Academy Theater...")
	screenings := []Screening{}
	page := browser.MustPage("https://academytheaterpdx.com/revivalseries/").MustWaitStable()
	defer page.MustClose()
	eventEls := page.MustElements("div.at-np-bot-pad.at-np-container")
	filmUrls := []string{}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		log.Printf("Could not load theater time zone location")
		return screenings
	}
	for i, eventEl := range eventEls {
		log.Printf("Movie #%d", i+1)
		titleEl := eventEl.MustElement("div.at-np-details-title a")
		url := titleEl.MustAttribute("href")
		filmUrls = append(filmUrls, *url)
	}
	for _, url := range filmUrls {
		log.Printf("Url: %s", url)
		filmPage := browser.MustPage(url).MustWaitStable()
		title := filmPage.MustElement("div.entry-info h1.entry-title").MustText()
		log.Printf("Title: %s", title)
		showtimeDiv := filmPage.MustElement("div.entry-showtime div.showtime")
		dayEls := showtimeDiv.MustElements("div.st-title:not(.passedshowtime)")
		for _, dayEl := range dayEls {
			log.Println(dayEl)
			if !dayEl.MustVisible() {
				continue
			}
			day := dayEl.MustText()
			log.Printf("Day: %s", day)
			timesEl, _ := dayEl.Next() // Could be nil I think
			if timesEl == nil || !timesEl.MustVisible() {
				continue
			}
			for _, spanEl := range timesEl.MustElements("span") {
				rawTime := spanEl.MustText()
				log.Printf("Raw time: %s", rawTime)
				normalizedTime := strings.TrimSpace(rawTime) + " " + strings.TrimSpace(day)
				result, err := time.ParseInLocation("3:04 PM January _2, 2006", normalizedTime, location)
				if err != nil {
					log.Printf("Cannot parse time: %s", normalizedTime)
					var parseErr *time.ParseError
					if errors.As(err, &parseErr) {
						log.Print(parseErr)
					}
					continue
				}
				s := Screening{title: title, time: result, url: url, theater: "Academy Theater"}
				screenings = append(screenings, s)
			}
		}
	}
	return screenings
}

func scrapeCineMagicTheater(browser *rod.Browser) []Screening {
	log.Printf("Scraping CineMagic Theater...")
	screenings := []Screening{}
  url := "https://tickets.thecinemagictheater.com/now-showing"
	page := browser.MustPage(url).MustWaitStable()
	defer page.MustClose()
	calendarListEls := page.MustElements("div.calendar-filter li:not(.calendar)")
	for _, calListEl := range calendarListEls {
		date := calListEl.MustText()
		log.Printf("date: '%s'", date)
		date = strings.Replace(date, "Today", "", 1)
		date = strings.TrimSpace(date)
		dateTokens := strings.SplitAfter(date, "\n")
		log.Printf("dateTokens: %s", dateTokens)
		if len(dateTokens) != 3 {
			log.Printf("Wrong number of date tokens")
			continue
		}
		weekday := strings.TrimSpace(dateTokens[0])
		dayNum := strings.TrimSpace(dateTokens[1])
		month := strings.TrimSpace(dateTokens[2])
		year := time.Now().Local().Year()
		if time.Now().Local().Month() == time.December && month == "Jan" {
			year++
		}
    yearStr := strconv.Itoa(year)
		if !slices.Contains(shortWeekdays, weekday) {
			log.Printf("Weekday is not valid")
			continue
		}
		calListEl.MustClick().MustWaitStable()
		title := page.MustElement("div.movie-container div.text-white div.text-h5").MustText()
		log.Printf("Title is: '%s'", title)
		timeEls := page.MustElements("div.movie-container div.showings.col.row button")
		for _, timeEl := range timeEls {
			timeStr := timeEl.MustText()
			log.Printf("timeStr: '%s'", timeStr)
			assembledTime := month + " " + dayNum + " " + yearStr + " " + timeStr
			time, err := getTime("Jan _2 2006 3:04 PM", assembledTime, locations["Portland"])
      if err != nil { continue }
      screenings = append(screenings, Screening{time: time, title: title, url: url, theater: "CineMagic Theater"})
		}
	}
	return screenings
}

func main() {
	fmt.Printf("Starting movie-cal...\n")
	ensureDirs()
	browser := getBrowser()
	defer browser.MustClose()
	screenings := []Screening{}
	// screenings = append(screenings, scrapeClintonStateTheater()...)
	// screenings = append(screenings, scrapeHollywoodTheater(browser)...)
	// screenings = append(screenings, scrapeAcademyTheater(browser)...)
	screenings = append(screenings, scrapeCineMagicTheater(browser)...)
	sort.Slice(screenings, func(i, j int) bool {
		return screenings[i].time.Before(screenings[j].time)
	})
	printScreenings(screenings)
}
