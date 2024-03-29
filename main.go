package main

import (
	"errors"
	"fmt"
	"log"
	// "net/url"
	"flag"
	"os" // https://pkg.go.dev/os
	"runtime"
	"runtime/pprof"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
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
		println("TIME: " + s.time.In(tz).Format("Mon Jan _2 3:04 PM MST 2006"))
		println("THEATER: " + s.theater)
		println("URL: " + s.url)
		println("- [ ] " + s.time.Format("15:04") + " [" + s.title + " Screening](" + s.url + ") 📅 " + s.time.Format("2006-01-02"))
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

func scrapeClintonStateTheater(ch chan<- Screening, wgParent *sync.WaitGroup) {
	defer wgParent.Done()
	log.Printf("Scraping Clinton State Theater...")
	filename, err := downloadFile("cstpdx.ics", "https://cstpdx.com/schedule/list/?ical=1")
	if err != nil {
		log.Fatal(err)
	}
	cal, err := openIcsFile(filename)
	// cal.SerializeTo(os.Stdout)
	for _, e := range cal.Events() {
		t, err := e.GetStartAt()
		summary := e.GetProperty("SUMMARY")
		url := e.GetProperty("URL")
		if err != nil {
			continue
		}
		if t.Before(time.Now()) {
			continue
		}
		s := Screening{
			title:   strings.TrimSpace(summary.Value),
			time:    t,
			theater: "Clinton State Theater",
			url:     url.Value,
		}
		ch <- s
	}
	log.Printf("Finished Clinton State Theater")
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

func scrapeEventGrid(eventGridItemEls rod.Elements, ch chan<- Screening, wgParent *sync.WaitGroup) {
	defer wgParent.Done()
	for i, eventGridItemEl := range eventGridItemEls {
		log.Println("Starting an event grid scan for Hollywood Theater")
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
			t, err := time.ParseInLocation("3:04 PM January _2 2006", normalizedTime, location)
			if err != nil {
				log.Printf("Cannot parse time: %s", normalizedTime)
				var parseErr *time.ParseError
				if errors.As(err, &parseErr) {
					log.Print(parseErr)
				}
				continue
			}
			// If time is 6 months behind current date or more, assume it's in the next year
			if time.Now().Sub(t) > (time.Hour * 24 * 30 * 6) {
				t = t.AddDate(1, 0, 0)
			}
			if t.Before(time.Now()) {
				continue
			}
			url := timeEl.MustAttribute("href")
			s := Screening{
				title:   strings.TrimSpace(title),
				time:    t,
				theater: "Hollywood Theater",
				url:     "https://hollywoodtheatre.org" + *url,
			}
			ch <- s
		}
	}
}

func scrapeHollywoodTheater(bpool *rod.BrowserPool, ch chan<- Screening, wgParent *sync.WaitGroup) {
	defer wgParent.Done()
	log.Printf("Scraping Hollywood Theater...")
	browser := bpool.Get(getBrowser)
	defer bpool.Put(browser)
	page := browser.MustPage("https://hollywoodtheatre.org/").MustWaitStable()
	defer page.MustClose()
	eventGridItemEls := page.MustElements(".event-grid-item")
	wg := sync.WaitGroup{}
	wg.Add(1)
	go scrapeEventGrid(eventGridItemEls, ch, &wg)
	buttonEl, err := page.Element("a[data-events-target=\"comingSoonTab\"]")
	if err != nil {
		log.Printf("Cannot click \"Coming Soon\" button")
	}
	buttonEl.MustClick()
	eventGridItemEls = page.MustWaitStable().MustElements(".event-grid-item")
	wg.Add(1)
	go scrapeEventGrid(eventGridItemEls, ch, &wg)
	wg.Wait()
	log.Printf("Finished Hollywood Theater")
}

func scrapeAcademyTheater(bpool *rod.BrowserPool, ch chan<- Screening, wgParent *sync.WaitGroup) {
	defer wgParent.Done()
	log.Printf("Scraping Academy Theater...")
	browser := bpool.Get(getBrowser)
	defer bpool.Put(browser)
	page := browser.MustPage("https://academytheaterpdx.com/revivalseries/").MustWaitStable()
	defer page.MustClose()
	eventEls := page.MustElements("div.at-np-bot-pad.at-np-container")
	filmUrls := []string{}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		log.Printf("Could not load theater time zone location")
		return
	}
	for i, eventEl := range eventEls {
		log.Printf("Movie #%d", i+1)
		titleEl := eventEl.MustElement("div.at-np-details-title a")
		url := titleEl.MustAttribute("href")
		filmUrls = append(filmUrls, *url)
	}
	wg := sync.WaitGroup{}
	for _, url := range filmUrls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			log.Println("Starting a request for Academy Theater")
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
				day := dayEl.MustElement("label").MustText()
				log.Printf("Day: %s", day)
				timesEl, _ := dayEl.Next() // Could be nil I think
				if timesEl == nil || !timesEl.MustVisible() {
					continue
				}
				for _, spanEl := range timesEl.MustElements("span") {
					rawTime := spanEl.MustText()
					log.Printf("Raw time: %s", rawTime)
					normalizedTime := strings.TrimSpace(rawTime) + " " + strings.TrimSpace(day)
					t, err := time.ParseInLocation("3:04 PM January _2, 2006", normalizedTime, location)
					if err != nil {
						log.Printf("Cannot parse time: %s", normalizedTime)
						var parseErr *time.ParseError
						if errors.As(err, &parseErr) {
							log.Print(parseErr)
						}
						continue
					}
					if t.Before(time.Now()) {
						continue
					}
					newScreening := Screening{
						title:   strings.TrimSpace(title),
						time:    t,
						url:     url,
						theater: "Academy Theater",
					}
					ch <- newScreening
				}
			}
		}(url)
	}
	wg.Wait()
	log.Printf("Finished Academy Theater")
}

func scrapeCineMagicTheater(bpool *rod.BrowserPool, ch chan<- Screening, wgParent *sync.WaitGroup) {
	defer wgParent.Done()
	log.Printf("Scraping CineMagic Theater...")
	browser := bpool.Get(getBrowser)
	defer bpool.Put(browser)
	url := "https://tickets.thecinemagictheater.com/now-showing"
	page := browser.MustPage(url).MustWaitStable()
	defer page.MustClose()
	calendarListEls := page.MustElements("div.calendar-filter li:not(.calendar)")
	wg := sync.WaitGroup{}
	for _, calListEl := range calendarListEls {
		wg.Add(1)
		go func(calListEl *rod.Element) {
			defer wg.Done()
			log.Println("Starting a request for cinemagic")
			date := calListEl.MustText()
			log.Printf("date: '%s'", date)
			date = strings.Replace(date, "Today", "", 1)
			date = strings.TrimSpace(date)
			dateTokens := strings.SplitAfter(date, "\n")
			log.Printf("dateTokens: %s", dateTokens)
			if len(dateTokens) != 3 {
				log.Printf("Wrong number of date tokens")
				return
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
				return
			}
			calListEl.MustClick().MustWaitStable()
			title := page.MustElement("div.movie-container div.text-white div.text-h5").MustText()
			log.Printf("Title is: '%s'", title)
			timeEls := page.MustElements("div.movie-container div.showings.col.row button")
			for _, timeEl := range timeEls {
				timeStr := timeEl.MustText()
				log.Printf("timeStr: '%s'", timeStr)
				assembledTime := month + " " + dayNum + " " + yearStr + " " + timeStr
				t, err := getTime("Jan _2 2006 3:04 PM", assembledTime, locations["Portland"])
				if err != nil {
					continue
				}
				if t.Before(time.Now()) {
					continue
				}
				newScreening := Screening{
					time:    t,
					title:   strings.TrimSpace(title),
					url:     url,
					theater: "CineMagic Theater",
				}
				ch <- newScreening
			}
		}(calListEl)
	}
	wg.Wait()
	log.Printf("Finished CineMagic Theater")
}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

func main() {

	// Profiling setup
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	fmt.Printf("Starting movie-cal...\n")
	fmt.Printf("GOMAXPROCS default is %d\n", runtime.NumCPU())
	ensureDirs()
	bpool := rod.NewBrowserPool(12)
	// we create a buffered channel so writing to it won't block while we wait for the waitgroup to finish
	ch := make(chan Screening, 1000)
	// we create a waitgroup - basically block until N tasks say they are done
	wg := sync.WaitGroup{}
	wg.Add(1)
	go scrapeClintonStateTheater(ch, &wg)
	wg.Add(1)
	go scrapeHollywoodTheater(&bpool, ch, &wg)
	wg.Add(1)
	go scrapeAcademyTheater(&bpool, ch, &wg)
	wg.Add(1)
	go scrapeCineMagicTheater(&bpool, ch, &wg)
	// now we wait for everyone to finish - again, not a must.
	// you can just receive from the channel N times, and use a timeout or something for safety
	log.Println("Start waiting")
	wg.Wait()
	log.Println("Done waiting")
	// we need to close the channel or the following loop will get stuck
	close(ch)
	// screenings = append(screenings, scrapeCineMagicTheater(browser)...)
	// we iterate over the closed channel and receive all data from it
	screenings := []Screening{}
	for screening := range ch {
		screenings = append(screenings, screening)
	}
	sort.Slice(screenings, func(i, j int) bool {
		return screenings[i].time.After(screenings[j].time)
	})
	printScreenings(screenings)

	// Profiling cleanup
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}
}
