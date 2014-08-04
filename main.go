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
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const (
	vkAddr   = "http://vk.com/"
	layout   = "2006-01-02:15:04:05"
	extData  = ".dat"
	extGraph = ".svg"
)

var (
	postIDRe = regexp.MustCompile(`wall-[^"]*offset=last&f=replies`)
	wallIDRe = regexp.MustCompile(`wall-[^\'][0-9]*`)
	dict     = []string{"бомб", "град", "взрыв", "взор", "ракет", "стрел", "карло"}
	v        = url.Values{"own": {"1"}}
	finished = make(chan int)
	logfn    = flag.String("log", "", "File to output data to (default: $PAGENAME.dat)")
	period   = flag.Float64("period", 120.0, "Update period (s)")
	X        = make([]float64, 0, 1000)
	Y        = make([]float64, 0, 1000)
)

func saveSvg(X, Y []float64, name string) {

	pts := make(plotter.XYs, len(Y))
	for i, _ := range Y {
		pts[i].X = X[i]
		pts[i].Y = Y[i]
	}

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	p.Title.Text = time.Now().String()
	p.X.Label.Text = "Час (хв.)"
	p.Y.Label.Text = "Кількість посилань"

	err = plotutil.AddLinePoints(p,
		"м. Єнакієве (typical_enakievo)", pts)
	if err != nil {
		panic(err)
	}

	c := plotter.NewFunction(func(x float64) float64 { return 50.0 })
	c.Color = color.RGBA{B: 255, A: 255}
	c.Dashes = []vg.Length{vg.Points(4), vg.Points(5)}

	p.Add(c)
	p.Y.Min = 0.0
	p.Y.Max = 100.0

	// Save the plot to a PNG file.
	if err := p.Save(10, 5, name+extGraph); err != nil {
		panic(err)
	}

}

func stopOnError(err error) {
	if err != nil {
		log.Panicln(err)
		os.Exit(1)
	}
}

func getWallID(u string, v url.Values) string {
	resp, _ := http.PostForm(u, v)
	body, _ := ioutil.ReadAll(resp.Body)
	sbody := string(body)
	return wallIDRe.FindAllString(sbody, -1)[0]
}

func getPostIDs(wallID string, v url.Values) []string {
	resp, _ := http.PostForm(vkAddr+wallID, v)
	body, _ := ioutil.ReadAll(resp.Body)
	sbody := string(body)
	return postIDRe.FindAllString(sbody, -1)
}

func countMatches(postID string, count *int64, finished chan int) {
	postURL, _ := url.Parse(vkAddr + postID)
	postBaseURL := vkAddr + postURL.Path
	resp, _ := http.PostForm(postBaseURL, postURL.Query())
	rInUTF8 := transform.NewReader(resp.Body, charmap.Windows1251.NewDecoder())
	body, _ := ioutil.ReadAll(rInUTF8)
	s := strings.ToLower(string(body))
	for _, word := range dict {
		atomic.AddInt64(count, int64(strings.Count(s, word)))
	}
	finished <- 1
}

func main() {

	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()
	pageName := flag.Args()[0]
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
	x := 0.0
	for {
		postIDs := getPostIDs(wallID, v)
		count := int64(0)
		for _, postID := range postIDs {
			go countMatches(postID, &count, finished)
		}
		time.Sleep(time.Duration(*period) * time.Second)
		for _, _ = range postIDs {
			<-finished
		}
		x += 0.5
		X = append(X, x)
		Y = append(Y, float64(count))
		fmt.Fprintf(f, "%s\t%d\n", time.Now().Format(layout), count)
		saveSvg(X, Y, pageName)
	}
}
