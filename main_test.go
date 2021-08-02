package main

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAnalyse_Start(t *testing.T) {
	logDatas := []string{
		"nihao",
		"bbbbm",
		"2021-08-01 18:07:34.789 [INFO]  [Core] ^[[31;1m@chain1^[[0m     common/block_helper.go:760      remove txs[3] and retry txs[0] in add block",
		"2021-08-01 18:19:37.514 [INFO]  [Core] @chain1  common/block_helper.go:767      commit block [708](count:500,hash:b8c17b26a0a5cbf4c8a21a7ad70650f270e8ae0feb779cca20c978c691e5ff44), time used(check:0,db:0,ss:0,conf:0,pool:0,pubConEvent:0,other:0,total:0)",
		"2021-08-01 18:19:15.442 [INFO]  [Core] @chain1  common/block_helper.go:767      commit block [686](count:436,hash:f79ac21be49e54f072274f262e85190bd6b4f9a5fedccef2611707a977dce343), time used(check:0,db:0,ss:0,conf:0,pool:0,pubConEvent:0,other:0,total:0)",
		"2021-08-01 18:19:16.434 [INFO]  [Core] @chain1  common/block_helper.go:767      commit block [687](count:32843,hash:82698a4136dd232bba70883f36963f020c4d5c2994a7311f001e0d14d063e59b), time used(check:0,db:0,ss:0,conf:0,pool:0,pubConEvent:0,other:0,total:0)",
		"2021-08-01 18:19:17.437 [INFO]  [Core] @chain1  common/block_helper.go:767      commit block [688](count:321329,hash:712cdb784ff950f5a2f8c2843a596d7ddc35b703621975ac935266eb563038d8), time used(check:0,db:0,ss:0,conf:0,pool:0,pubConEvent:0,other:0,total:0)",
	}

	commitBlkMatchRegexp, err := regexp.Compile("commit block ")
	require.NoError(t, err)
	blockTxCountRegexp, err := regexp.Compile("count:[0-9]+")
	require.NoError(t, err)

	for _, log := range logDatas {
		if !commitBlkMatchRegexp.MatchString(log) {
			continue
		}
		fmt.Println(log)
		txCountStr := blockTxCountRegexp.FindString(log)
		fmt.Println(txCountStr)
	}
}

func TestParseTime(t *testing.T) {
	//data := "2021-08-01 18:19:26.489"
	//timeData, err := time.Parse("2006-01-02 15:04:05.000", data)
	data := "2021080118"
	timeData, err := time.Parse("2006010215", data)
	require.NoError(t, err)
	fmt.Println(timeData.String())
}
