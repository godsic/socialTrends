package main

import (
	"code.google.com/p/go.text/encoding/charmap"
	"code.google.com/p/go.text/transform"
	"code.google.com/p/plotinum/plot"
	"code.google.com/p/plotinum/plotter"
	"code.google.com/p/plotinum/plotutil"
	"code.google.com/p/plotinum/vg"
	"flag"
	"fmt"
	"image/color"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	// "strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	vkAddr          = "http://vk.com/"
	layout          = "2006-01-02:15:04:05"
	svgTimeLayout   = "15:04:05 -0700 EDT Jan 2 2006"
	extData         = ".dat"
	extGraph        = ".svg"
	defaultPageName = "typical_enakievo"
	imageName       = "status"
	safeLimit       = 70
	headroom        = 10
)

var (
	postIDRe = regexp.MustCompile(`wall-[^"]*offset=last&f=replies`)
	wallIDRe = regexp.MustCompile(`wall-[^\'][0-9]*`)
	dict     = []string{"бомб", "град", "взрыв", "взор", "ракет", "стрел", "мином", "мина", "самол"}
	v        = url.Values{"own": {"1"}}
	finished = make(chan int)
	logfn    = flag.String("log", "", "File to output data to (default: $PAGENAME.dat)")
	period   = flag.Float64("period", 30.0, "Update period (s)")
	X        = make([]float64, 0, 3000)
	Y        = make([]float64, 0, 3000)
)

func saveSvg(X, Y []float64, name string, minY, maxY float64) {

	pts := make(plotter.XYs, len(Y))
	for i, _ := range Y {
		pts[i].X = X[i]
		pts[i].Y = Y[i]
	}

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	p.Title.Text = time.Now().Format(svgTimeLayout)
	p.X.Label.Text = "Час (хв.)"
	p.Y.Label.Text = "Відхилення від середньої кількості посилань"

	err = plotutil.AddLinePoints(p,
		name, pts)
	if err != nil {
		panic(err)
	}

	cUp := plotter.NewFunction(func(x float64) float64 { return math.Abs(safeLimit) })
	cUp.Color = color.RGBA{B: 255, A: 255}
	cUp.Dashes = []vg.Length{vg.Points(4), vg.Points(5)}
	p.Add(cUp)
	p.Legend.Add("Безпечний рівень", cUp)
	p.Legend.Padding = vg.Length(5)

	cDown := plotter.NewFunction(func(x float64) float64 { return -math.Abs(safeLimit) })
	cDown.Color = color.RGBA{B: 255, A: 255}
	cDown.Dashes = []vg.Length{vg.Points(4), vg.Points(5)}
	p.Add(cDown)

	p.Y.Min = minY
	p.Y.Max = maxY
	p.X.Max = 0.0
	p.X.Min = -480.0
	// Save the plot to a PNG file.
	if err := p.Save(10, 5, imageName+extGraph); err != nil {
		panic(err)
	}

}

func stopOnError(err error) {
	if err != nil {
		log.Panicln(err)
		os.Exit(1)
	}
}

func printIfError(err error) {
	if err != nil {
		log.Println(err)
	}
}

func getWallID(u string, v url.Values) string {
	resp, err := http.PostForm(u, v)
	defer resp.Body.Close()
	printIfError(err)
	body, err := ioutil.ReadAll(resp.Body)
	printIfError(err)
	sbody := string(body)
	return wallIDRe.FindAllString(sbody, -1)[0]
}

func getPostIDs(wallID string, v url.Values) []string {
	resp, err := http.PostForm(vkAddr+wallID, v)
	defer resp.Body.Close()
	printIfError(err)
	body, err := ioutil.ReadAll(resp.Body)
	printIfError(err)
	sbody := string(body)
	return postIDRe.FindAllString(sbody, -1)
}

func countMatches(postID string, count *int64, finished chan int) {
	postURL, _ := url.Parse(vkAddr + postID)
	postBaseURL := vkAddr + postURL.Path
	resp, err := http.PostForm(postBaseURL, postURL.Query())
	defer func() {
		resp.Body.Close()
		finished <- 1
	}()
	printIfError(err)
	rInUTF8 := transform.NewReader(resp.Body, charmap.Windows1251.NewDecoder())
	body, err := ioutil.ReadAll(rInUTF8)
	printIfError(err)
	s := strings.ToLower(string(body))
	for _, word := range dict {
		atomic.AddInt64(count, int64(strings.Count(s, word)))
	}
}

func startServer(addr string) {
	log.Panic(http.ListenAndServe(addr, nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func fillXAxis(X []float64, step float64) {
	N := len(X) - 1
	for i, _ := range X {
		X[i] = step * float64(i-N)
	}
}

func main() {

	runtime.GOMAXPROCS(runtime.NumCPU())

	http.HandleFunc("/", rootHandler)

	addr := fmt.Sprintf("%s:%s", os.Getenv("HOST"), os.Getenv("PORT"))
	if addr == ":" || len(addr) == 0 {
		addr = ":8080"
	}

	http.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir("."))))

	go startServer(addr)

	flag.Parse()

	pageName := defaultPageName

	if len(flag.Args()) > 0 {
		pageName = flag.Args()[0]
	}

	log.Println(pageName)

	u := vkAddr + pageName
	wallID := getWallID(u, v)
	log.Println(wallID)

	if *logfn == "" {
		*logfn = pageName + extData
	}

	f, err := os.OpenFile(*logfn, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
	defer f.Close()
	stopOnError(err)
	minY := 1.0e200
	maxY := -1.0e200
	for {
		log.Println("extract comments...")
		postIDs := getPostIDs(wallID, v)
		count := int64(0)
		log.Println("searching for matches...")
		for _, postID := range postIDs {
			go countMatches(postID, &count, finished)
		}
		log.Println("waiting...")
		time.Sleep(time.Duration(*period) * time.Second)
		for _, _ = range postIDs {
			<-finished
		}

		log.Println("saving data...")
		_, err := fmt.Fprintf(f, "%s\t%d\n", time.Now().Format(layout), count)
		printIfError(err)
		X = append(X, 0.0)
		fillXAxis(X, *period/60.0)
		// get Y average
		y := float64(count)
		if y > maxY {
			maxY = y
		}
		if y < minY {
			minY = y
		}
		//
		Y = append(Y, y)
		log.Println("rendering data...")
		saveSvg(X, Y, pageName, minY-headroom, maxY+headroom)
	}
}
