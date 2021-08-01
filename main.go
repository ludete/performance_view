package main

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/hpcloud/tail"
)

func main() {
	file, err := tail.TailFile("", tail.Config{Follow: true})
	if err != nil {
		panic(err)
	}

	regexpMatch, err := regexp.Compile("commit block ")
	if err != nil {
		panic(err)
	}
	blockTxCountRegexp, err := regexp.Compile("count:*,")
	if err != nil {
		panic(err)
	}

	format := "2006-01-02 15:04:05.000"
	timeLength := len(format)
	var (
		totalCount = 0
		start      *time.Time
		lastTime   *time.Time
		records    = make(map[int]int, 10)
	)

	for line := range file.Lines {
		if ok := regexpMatch.MatchString(line.Text); ok {
			fmt.Println(line.Text)
			lineTime, err := time.Parse(format, line.Text[:timeLength])
			if err != nil {
				panic(fmt.Errorf("parse time: %s error: %s", line.Text[:timeLength], err))
			}
			txCountRecord := blockTxCountRegexp.FindString(line.Text)
			if len(txCountRecord) == 0 {
				panic(fmt.Errorf("not find txCount in log: %s", line.Text))
			}
			txCountStr := txCountRecord[6 : len(txCountRecord)-1]
			txCount, err := strconv.Atoi(txCountStr)
			if err != nil {
				panic(fmt.Errorf("parse txcount: %s faile: %s", txCountStr, err))
			}

			// calculate logic
			if lastTime == nil {
				lastTime = &lineTime
			}
			if start == nil {
				start = &lineTime
			}
			totalCount += txCount
			if escaped := lineTime.Unix() - lastTime.Unix(); escaped >= 30 {
				rate := totalCount / int(escaped)
				records[int(lineTime.Unix()-start.Unix())] = rate
				totalCount = 0
				lastTime = nil
			}

			// view change
		}
	}

}
