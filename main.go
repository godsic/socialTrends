package main

import (
	"code.google.com/p/go.text/encoding/charmap"
	"code.google.com/p/go.text/transform"
	"flag"
	"fmt"
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
	vkAddr = "http://vk.com/"
	layout = "2006-01-02:15:04:05"
	ext    = ".dat"
)

var (
	postIDRe = regexp.MustCompile(`wall-[^"]*offset=last&f=replies`)
	wallIDRe = regexp.MustCompile(`wall-[^\'][0-9]*`)
	dict     = []string{"бомб", "град", "взрыв", "взор", "ракет", "стрел", "карло"}
	v        = url.Values{"own": {"1"}}
	finished = make(chan int)
	logfn    = flag.String("log", "", "File to output data to (default: $PAGENAME.dat)")
	period   = flag.Float64("period", 120.0, "Update period (s)")
)

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
		*logfn = pageName + ext
	}

	f, err := os.OpenFile(*logfn, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0755)
	defer f.Close()
	stopOnError(err)

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
		fmt.Fprintf(f, "%s\t%d\n", time.Now().Format(layout), count)
	}
}
