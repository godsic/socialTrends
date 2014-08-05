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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	vkAddr          = "http://vk.com/"
	layout          = "2006-01-02:15:04:05"
	svgTimeLayout   = "15:04:05 -0700 EEST Jan 2 2006"
	extData         = ".dat"
	extGraph        = ".svg"
	defaultPageName = "typical_enakievo"
	safeLimit       = 10
)

var (
	postIDRe = regexp.MustCompile(`wall-[^"]*offset=last&f=replies`)
	wallIDRe = regexp.MustCompile(`wall-[^\'][0-9]*`)
	dict     = []string{"бомб", "град", "взрыв", "взор", "ракет", "стрел", "карло", "енак", "горлов", "енач"}
	v        = url.Values{"own": {"1"}}
	finished = make(chan int)
	logfn    = flag.String("log", "", "File to output data to (default: $PAGENAME.dat)")
	period   = flag.Float64("period", 30.0, "Update period (s)")
	X        = make([]float64, 0, 3000)
	Y        = make([]float64, 0, 3000)
)

func saveSvg(X, Y []float64, name string, avgNumPosts float64) {

	pts := make(plotter.XYs, len(Y))
	for i, _ := range Y {
		pts[i].X = X[i]
		pts[i].Y = Y[i]
	}

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	p.Title.Text = time.Now().Format(svgTimeLayout) + " Середня кількість посилань: " + strconv.FormatFloat(avgNumPosts, 'f', -1, 64)
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

	p.Y.Min = -20.0
	p.Y.Max = 20.0
	p.X.Max = 0.0
	p.X.Min = -500.0
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
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	sbody := string(body)
	return wallIDRe.FindAllString(sbody, -1)[0]
}

func getPostIDs(wallID string, v url.Values) []string {
	resp, _ := http.PostForm(vkAddr+wallID, v)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	sbody := string(body)
	return postIDRe.FindAllString(sbody, -1)
}

func countMatches(postID string, count *int64, finished chan int) {
	postURL, _ := url.Parse(vkAddr + postID)
	postBaseURL := vkAddr + postURL.Path
	resp, _ := http.PostForm(postBaseURL, postURL.Query())
	defer resp.Body.Close()
	rInUTF8 := transform.NewReader(resp.Body, charmap.Windows1251.NewDecoder())
	body, _ := ioutil.ReadAll(rInUTF8)
	s := strings.ToLower(string(body))
	for _, word := range dict {
		atomic.AddInt64(count, int64(strings.Count(s, word)))
	}
	finished <- 1
}

func startServer(addr string) {
	log.Panic(http.ListenAndServe(addr, nil))
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func rebaseXAxis(X []float64, x0 float64) []float64 {
	XX := make([]float64, len(X))
	for i, _ := range X {
		XX[i] = X[i] - x0
	}
	return XX
}

func rebaseYAxis(Y []float64, y0 float64) []float64 {
	YY := make([]float64, len(Y))
	for i, _ := range Y {
		YY[i] = Y[i] - y0
	}
	return YY
}

func getAverageArray(in []float64) (val float64) {
	val = 0.0
	for _, v := range in {
		val += v
	}
	val /= float64(len(in))
	return
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
		y := getAverageArray(Y)
		saveSvg(rebaseXAxis(X, x), rebaseXAxis(Y, y), pageName, y)
	}
}
