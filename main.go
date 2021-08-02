package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
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

	analyseLogic := NewAnalyse(dir, "system.log", "2006010215", "2006-01-02 15:04:05.000", collectTime)
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
	fileTimeFormat string
	lineTimeFormat string
	collectionTime int64

	tailFile             *tail.Tail
	blockTxCountRegexp   *regexp.Regexp
	commitBlkMatchRegexp *regexp.Regexp
}

func NewAnalyse(dir, filePrefix, fileTimeFormat, lineTimeFormat string, collectT int) *Analyse {
	return &Analyse{
		dir:            dir,
		filePrefix:     filePrefix,
		fileTimeFormat: fileTimeFormat,
		lineTimeFormat: lineTimeFormat,
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
	defer func() {
		fmt.Println("next read file: ", a.lastReadFile)
	}()

	for {
		fileNames := a.sortFiles(a.readDir())
		fmt.Println(fileNames)
		for i, name := range fileNames {
			if name == a.lastReadFile {
				if i == len(fileNames)-1 {
					fmt.Println("wait next file ....")
					time.Sleep(time.Second)
					break
				} else {
					a.lastReadFile = fileNames[i+1]
					return
				}
			}
			if a.lastReadFile == "" {
				a.lastReadFile = name
				return
			}
		}
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
		left, err := time.Parse(a.fileTimeFormat, leftFileTime) // utc
		if err != nil {
			panic(fmt.Sprintf("parse time: %s error, failed reason: %s", leftFileTime, err))
		}
		right, err := time.Parse(a.fileTimeFormat, rightFileTime) // utc
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

		//format     = "2006-01-02 15:04:05.000"
		timeLength = len(a.lineTimeFormat)
		ticker     = time.NewTicker(time.Second)

		firstComplete bool
	)
	defer ticker.Stop()
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}

	for {
		select {
		case line := <-a.tailFile.Lines:
			if ok := a.commitBlkMatchRegexp.MatchString(line.Text); ok {
				//fmt.Println(line.Text)
				lineTime, err := time.Parse(a.lineTimeFormat, line.Text[:timeLength])
				if err != nil {
					panic(fmt.Errorf("parse time: %s error: %s", line.Text[:timeLength], err))
				}
				txCountRecord := a.blockTxCountRegexp.FindString(line.Text)
				if len(txCountRecord) == 0 {
					panic(fmt.Errorf("not find txCount in log: %s", line.Text))
				}
				txCount, err := strconv.Atoi(txCountRecord[6:])
				if err != nil {
					panic(fmt.Errorf("parse txcount: %s failed reason: %s", txCountRecord[6:], err))
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
			if len(a.lastReadFile) == 0 {
				continue
			}
			offset, err := a.tailFile.Tell()
			if err != nil {
				panic(err)
			}
			if fileInfo, err := os.Stat(filepath.Join(a.dir, a.lastReadFile)); err == nil && offset < fileInfo.Size() {
				fmt.Println("file not read complete")
				continue
			} else if err != nil {
				panic(err)
			}

			if !firstComplete {
				firstComplete = true
				continue
			}
			//fileCreateTime, err := time.Parse(a.timeFormat, a.lastReadFile[len(a.timeFormat)+1:])
			fileCreateTime, err := time.ParseInLocation(a.fileTimeFormat, a.lastReadFile[len(a.filePrefix)+1:], l)
			if err != nil {
				panic(err)
			}
			fmt.Println("now: ", time.Now().String(), ", fileCreateTime: ", fileCreateTime.String(), ", diff: ", time.Now().Unix()-fileCreateTime.Unix())
			if diff := time.Now().Unix() - fileCreateTime.Unix(); diff >= 3600 {
				if err := a.tailFile.Stop(); err != nil {
					panic(err)
				}
				fmt.Printf("stop tail file: %s \n\n", a.lastReadFile)
				return
			} else {
				fmt.Printf("time diff: %d \n\n", diff)
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
