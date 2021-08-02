package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
	"github.com/hpcloud/tail"
)

func main() {
	var (
		dir         string
		collectTime int
	)
	flag.StringVar(&dir, "dir", "", "the log dir")
	flag.IntVar(&collectTime, "collectT", 10, "the collect time for performance tools")
	flag.Parse()
	if len(dir) == 0 {
		panic("the log dir is null")
	}

	analyseLogic := NewAnalyse(dir, "system.log", "2006010215", collectTime)
	if err := analyseLogic.Start(); err != nil {
		panic(err)
	}

	fmt.Println("listen on: 0.0.0.0:8081 ")
	http.HandleFunc("/", analyseLogic.viewPerformance)
	if err := http.ListenAndServe("0.0.0.0:8081", nil); err != nil {
		panic(err)
	}
}

type Analyse struct {
	mtx           sync.Mutex
	records       []Point
	lastReadFile  string
	firstBlkStart *time.Time

	dir            string
	filePrefix     string
	timeFormat     string
	collectionTime int64

	tailFile             *tail.Tail
	blockTxCountRegexp   *regexp.Regexp
	commitBlkMatchRegexp *regexp.Regexp
}

func NewAnalyse(dir, filePrefix, timeFormat string, collectT int) *Analyse {
	return &Analyse{
		dir:            dir,
		filePrefix:     filePrefix,
		timeFormat:     timeFormat,
		collectionTime: int64(collectT),
		records:        make([]Point, 0, 10000),
	}
}

func (a *Analyse) Start() error {
	var err error
	if a.blockTxCountRegexp, err = regexp.Compile("count:[0-9]+"); err != nil {
		return err
	}
	if a.commitBlkMatchRegexp, err = regexp.Compile("commit block "); err != nil {
		return err
	}
	go a.tailDir()
	return nil
}

func (a *Analyse) tailDir() {
	var err error
	for {
		a.getNextFile()
		if a.tailFile, err = tail.TailFile(filepath.Join(a.dir, a.lastReadFile), tail.Config{Follow: true, MustExist: true}); err != nil {
			panic(err)
		}
		a.analyseFile()
	}
}

func (a *Analyse) getNextFile() {
	for {
		fileNames := a.sortFiles(a.readDir())
		fmt.Println(fileNames)
		for i, name := range fileNames {
			if name == a.lastReadFile {
				if i == len(fileNames)-1 {
					time.Sleep(time.Minute)
					fmt.Println("wait next file ....")
					continue
				} else {
					a.lastReadFile = fileNames[i+1]
					break
				}
			}
			if a.lastReadFile == "" {
				a.lastReadFile = name
				break
			}
		}
		fmt.Println("next read file: ", a.lastReadFile)
		break
	}
}

func (a *Analyse) readDir() []string {
	files, err := ioutil.ReadDir(a.dir)
	if err != nil {
		panic(err)
	}
	names := make([]string, 0, len(files)-1)
	for _, info := range files {
		if info.Name() == a.filePrefix {
			continue
		}
		names = append(names, info.Name())
	}
	return names
}

// log name: system.log.2021080118
func (a *Analyse) sortFiles(fileNames []string) []string {
	sort.Slice(fileNames, func(i, j int) bool {
		leftFileTime := fileNames[i][len(a.filePrefix)+1:]
		rightFileTime := fileNames[i][len(a.filePrefix)+1:]
		left, err := time.Parse(a.timeFormat, leftFileTime)
		if err != nil {
			panic(fmt.Sprintf("parse time: %s error, failed reason: %s", leftFileTime, err))
		}
		right, err := time.Parse(a.timeFormat, rightFileTime)
		if err != nil {
			panic(fmt.Sprintf("parse time: %s error, failed reason: %s", rightFileTime, err))
		}
		return left.Unix() < right.Unix()
	})
	return fileNames
}

func (a *Analyse) analyseFile() {

	var (
		totalCount = 0
		lastTime   *time.Time

		format     = "2006-01-02 15:04:05.000"
		timeLength = len(format)
		ticker     = time.NewTicker(time.Second)
	)
	defer ticker.Stop()

	for {
		select {
		case line := <-a.tailFile.Lines:
			if ok := a.commitBlkMatchRegexp.MatchString(line.Text); ok {
				//fmt.Println(line.Text)
				lineTime, err := time.Parse(format, line.Text[:timeLength])
				if err != nil {
					panic(fmt.Errorf("parse time: %s error: %s", line.Text[:timeLength], err))
				}
				txCountRecord := a.blockTxCountRegexp.FindString(line.Text)
				if len(txCountRecord) == 0 {
					panic(fmt.Errorf("not find txCount in log: %s", line.Text))
				}
				txCountStr := txCountRecord[6:len(txCountRecord)]
				txCount, err := strconv.Atoi(txCountStr)
				if err != nil {
					panic(fmt.Errorf("parse txcount: %s failed reason: %s", txCountStr, err))
				}

				// calculate logic
				if lastTime == nil {
					lastTime = &lineTime
				}
				if a.firstBlkStart == nil {
					a.firstBlkStart = &lineTime
				}
				totalCount += txCount
				if escaped := lineTime.Unix() - lastTime.Unix(); escaped >= a.collectionTime {
					rate := totalCount / int(escaped)
					totalCount = 0
					lastTime = nil
					a.addPoint(int(lineTime.Unix()-a.firstBlkStart.Unix()), rate)
				}
				//else {
				//fmt.Println("lineTime: ", lineTime.String(), "lastTime: ", lastTime.String())
				//}
			}
		case <-ticker.C:
			fileCreateTime, err := time.Parse(a.timeFormat, a.lastReadFile[len(a.timeFormat)+1:])
			if err != nil {
				panic(err)
			}
			fmt.Println("now: ", time.Now().UTC().String(), "fileCreateTime: ", fileCreateTime.String(), "diff: ", time.Now().UTC().Unix()-fileCreateTime.Unix())
			if time.Now().UTC().Unix()-fileCreateTime.Unix() >= 3600 {
				if err := a.tailFile.Stop(); err != nil {
					panic(err)
				}
				fmt.Println("stop tail file: ", a.lastReadFile)
				return
			} else {
				fmt.Printf("time diff: %d \n\n", time.Now().UTC().Unix()-fileCreateTime.Unix())
			}
		}
	}
}

func (a *Analyse) addPoint(escaped int, performance int) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.records = append(a.records, Point{tPoint: escaped, pPoint: performance})
	//fmt.Println(a.records)
}

func (a *Analyse) viewPerformance(w http.ResponseWriter, _ *http.Request) {
	line := charts.NewLine()
	// set some global options like Title/Legend/ToolTip or anything else
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title: "ChainMaker performance test in guangzhou",
			//Subtitle: "ChainMaker Test",
		})).SetSeriesOptions(
		//charts.WithLineStyleOpts(),
		//charts.WithLineChartOpts(),

		charts.WithMarkLineNameXAxisItemOpts(opts.MarkLineNameXAxisItem{Name: "minute"}),
		charts.WithMarkLineNameTypeItemOpts(opts.MarkLineNameTypeItem{Name: "minute"}),
		charts.WithMarkLineNameYAxisItemOpts(opts.MarkLineNameYAxisItem{Name: "tps"}),
	)

	x, y := a.getXesAndYes()
	fmt.Println(x)
	line.SetXAxis(x).AddSeries("minute", y)
	if err := line.Render(w); err != nil {
		panic(err)
	}
}

type Point struct {
	tPoint int // time
	pPoint int // performance
}

func (a *Analyse) getXesAndYes() ([]int, []opts.LineData) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	x := make([]int, 0, len(a.records))
	y := make([]opts.LineData, 0, len(a.records))

	for _, p := range a.records {
		x = append(x, p.tPoint)
		y = append(y, opts.LineData{Value: p.pPoint})
	}
	return x, y
}
