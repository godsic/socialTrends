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
	vkAddr           = "http://vk.com/"
	layout           = "2006-01-02:15:04:05"
	svgTimeLayout    = "15:04:05 -0700 EDT Jan 2 2006"
	extData          = ".dat"
	extGraph         = ".svg"
	defaultPageName  = "typical_enakievo"
	imageName        = "status"
	safeLimit        = 70
	headroom         = 10
	postFormTimeout  = 5.0 * time.Second
	sleepIfPostFails = 2.0 * time.Second
	vgFontDir        = "/src/code.google.com/p/plotinum/vg/fonts"
)

var (
	postIDRe = regexp.MustCompile(`wall-[^"]*offset=last&f=replies`)
	wallIDRe = regexp.MustCompile(`wall-[^\'][0-9]*`)
	dict     = [][]string{{"бомб", "град", "взрыв", "взор", "ракет", "стрел", "мином", "мина", "самол", "снаряд", "пуля", "пули", "танк", "бмп", "бтр", "гауб", "броне"},
		{"укр", "нац", "карат", "армия", "правосек", "пиндос"},
		{"сепар", "ополчен", "комуфл", "ватник", "террор", "новорос", "днр", "лнр", "чечен", "козак", "казак"},
		{"карло", "веровк", "софиевк"},
		{"ольховат"},
		{"юнком"},
		{"булавин"},
		{"волынц"},
		{"еленовк", "александро"},
		{"ватут", "центр"},
		{"фильт", "блоч", "один два"},
		{"розовк", "жданов", "городок"},
		{"киров"},
		{"корсу", "старопетр"},
		{"крынка"},
		{"горлов"},
		{"углег"}}
	v             = url.Values{"own": {"1"}}
	finished      = make(chan int)
	logfn         = flag.String("log", "", "File to output data to (default: $PAGENAME.dat)")
	period        = flag.Float64("period", 30.0, "Update period (s)")
	X             = make([]float64, 0, 1000)
	Y             = make([][]float64, len(dict))
	count         = make([]int64, len(dict))
	DefaultDashes = [][]vg.Length{
		{},
		{vg.Points(1), vg.Points(1)},
		{vg.Points(3), vg.Points(1)},
		{vg.Points(5), vg.Points(1)}}
)

func flattenArrayStrings(in []string) string {
	str := ""
	for _, val := range in {
		str += (val + " ")
	}
	return str
}

func getPlotterFromSlice(X []float64, Y []float64) plotter.XYs {
	pts := make(plotter.XYs, len(Y))
	for i, _ := range Y {
		pts[i].X = X[i]
		pts[i].Y = Y[i]
	}
	return pts
}

func saveSvg(X []float64, Y [][]float64, name string, minY, maxY float64) {

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	p.Title.Text = vkAddr + name + " " + time.Now().Format(svgTimeLayout)
	p.X.Label.Text = "Час (хв.)"
	p.Y.Label.Text = "Кількість посилань"

	for i, y := range Y {
		line, err := plotter.NewLine(getPlotterFromSlice(X, y))
		if err != nil {
			panic(err)
		}
		line.Color = plotutil.Color(i)
		line.Dashes = DefaultDashes[i/7]
		p.Add(line)
		p.Legend.Add(flattenArrayStrings(dict[i]), line)
	}

	c := plotter.NewFunction(func(x float64) float64 { return math.Abs(safeLimit) })
	c.Color = color.RGBA{B: 255, A: 255}
	c.Dashes = []vg.Length{vg.Points(2), vg.Points(2)}
	c.Width = vg.Length(1)
	p.Add(c)

	p.Legend.Add("Безпечний рівень", c)
	p.Legend.Font.Size = vg.Length(6)
	p.Legend.Left = true

	p.Add(plotter.NewGrid())

	p.Y.Min = math.Min(minY, safeLimit-headroom)
	p.Y.Max = math.Max(maxY, safeLimit+headroom)
	p.X.Max = 0.0
	p.X.Min = -720.0
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

func countMatches(postID string, count []int64, finished chan int) {
	defer func() { finished <- 1 }()

	var (
		body []byte
		err  error
	)

	postURL, _ := url.Parse(vkAddr + postID)
	postBaseURL := vkAddr + postURL.Path

	client := &http.Client{Timeout: postFormTimeout}

	for {
		resp, err1 := client.PostForm(postBaseURL, postURL.Query())
		printIfError(err1)
		if err1 != nil {
			resp.Body.Close()
			goto SKIPBADREQ
		}
		body, err = ioutil.ReadAll(transform.NewReader(resp.Body, charmap.Windows1251.NewDecoder()))
		printIfError(err)
		resp.Body.Close()
		if err == nil {
			break
		}
	SKIPBADREQ:
		time.Sleep(sleepIfPostFails)
	}

	s := strings.ToLower(string(body))
	for i, subdict := range dict {
		for _, word := range subdict {
			atomic.AddInt64(&count[i], int64(strings.Count(s, word)))
		}
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
	log.Println(vg.FontDirs)
	fontdir := os.Getenv("GOPATH") + vgFontDir
	vg.FontDirs = []string{fontdir, "./", "./fonts"}
	log.Println(vg.FontDirs)

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
		for i, _ := range count {
			count[i] = int64(0)
		}
		log.Println("searching for matches...")
		for _, postID := range postIDs {
			go countMatches(postID, count, finished)
		}
		log.Println("waiting...")
		time.Sleep(time.Duration(*period) * time.Second)
		for _, _ = range postIDs {
			<-finished
		}

		X = append(X, 0.0)
		fillXAxis(X, *period/60.0)

		_, err := fmt.Fprintf(f, "%s\t", time.Now().Format(layout))
		printIfError(err)

		for i, _ := range count {
			_, err := fmt.Fprintf(f, "%d\t", count[i])
			printIfError(err)
			// get Y average
			y := float64(count[i])
			if y > maxY {
				maxY = y
			}
			if y < minY {
				minY = y
			}
			//
			Y[i] = append(Y[i], y)
		}
		_, err = fmt.Fprint(f, "\n")
		printIfError(err)
		saveSvg(X, Y, pageName, minY-headroom, maxY+headroom)
	}
}
