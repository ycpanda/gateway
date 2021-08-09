package index_diff

import (
	"fmt"
	"github.com/bsm/extsort"
	log "github.com/cihub/seelog"
	"infini.sh/framework/core/config"
	"infini.sh/framework/core/env"
	"infini.sh/framework/core/global"
	"infini.sh/framework/core/queue"
	"infini.sh/framework/core/util"
	"infini.sh/framework/lib/bytebufferpool"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

type CompareItem struct {
	Doc  interface{} `json:"doc,omitempty"`
	Key  string      `json:"key,omitempty"`
	Hash string      `json:"hash,omitempty"`
}

type DiffResult struct {
	DiffType string       `json:"type,omitempty"`
	Key      string       `json:"key,omitempty"`
	Source   *CompareItem `json:"source,omitempty"`
	Target   *CompareItem `json:"target,omitempty"`
}

func (a *CompareItem) CompareKey(b *CompareItem) int {
	v := strings.Compare(a.Key, b.Key)
	return v
}

func (a *CompareItem) CompareHash(b *CompareItem) int {
	return strings.Compare(a.Hash, b.Hash)
}

func NewCompareItem(key, hash string) CompareItem {
	item := CompareItem{Key: key, Hash: hash}
	return item
}

func processMsg(diffResultHandler func(DiffResult)) {
	var msgA, msgB CompareItem

MOVEALL:
	msgA = <-testChan.msgChans[diffConfig.GetSortedLeftQueue()]
	msgB = <-testChan.msgChans[diffConfig.GetSortedRightQueue()]
	//fmt.Println("Pop A:",msgA.Key)
	//fmt.Println("Pop B:",msgB.Key)

COMPARE:
	result := msgA.CompareKey(&msgB)

	//fmt.Println("A:",msgA.Key," vs B:",msgB.Key,"=",result)
	if global.Env().IsDebug {
		log.Trace(result, " - ", msgA, " vs ", msgB)
	}

	if result > 0 {

		result := DiffResult{Target: &msgB}
		result.DiffType = "OnlyInTarget"
		result.Key=msgB.Key

		diffResultHandler(result)

		if global.Env().IsDebug {
			log.Trace("OnlyInTarget :", msgB)
		}

		msgB = <-testChan.msgChans[diffConfig.GetSortedRightQueue()]
		//fmt.Println("Pop B:",msgB.Key)

		goto COMPARE
	} else if result < 0 { // 1 < 2

		result := DiffResult{Source: &msgA}
		result.DiffType = "OnlyInSource"
		result.Key=msgA.Key

		diffResultHandler(result)

		if global.Env().IsDebug {
			log.Trace(msgA, ": OnlyInSource")
		}

		msgA = <-testChan.msgChans[diffConfig.GetSortedLeftQueue()]
		//fmt.Println("Pop A:",msgA.Key)

		goto COMPARE
	} else {
		//doc exists, compare hash
		if msgA.CompareHash(&msgB) != 0 {
			if global.Env().IsDebug {
				log.Trace(msgA, "!=", msgB)
			}
			result := DiffResult{Target: &msgB, Source: &msgA}
			result.DiffType = "DiffBoth"
			result.Key=msgB.Key

			diffResultHandler(result)

		} else {
			if global.Env().IsDebug {
				log.Trace(msgA, "==", msgB)
			}
		}
		goto MOVEALL
	}
}

type IndexDiffModule struct {
}

type CompareChan struct {
	msgChans map[string]chan CompareItem
	stopChan chan struct{}
}

var testChan CompareChan

func (this IndexDiffModule) Name() string {
	return "index_diff"
}

type Config struct {
	Enabled            bool   `config:"enabled"`
	TextReportEnabled  bool   `config:"text_report"`
	KeepSourceInResult bool   `config:"keep_source"`
	BufferSize         int    `config:"buffer_size"`
	DiffQueue          string `config:"diff_queue"`
	SourceInputQueue    string         `config:"source_queue"`
	TargetInputQueue    string         `config:"target_queue"`
}

func (cfg *Config) GetSortedLeftQueue() string {
	return cfg.SourceInputQueue + "_sorted"
}

func (cfg *Config) GetSortedRightQueue() string {
	return cfg.TargetInputQueue + "_sorted"
}

var diffConfig = Config{
	TextReportEnabled: true,
	BufferSize:        1,
	SourceInputQueue:         "source",
	TargetInputQueue:         "target",
	DiffQueue:         "diff_result",
}

var wg sync.WaitGroup

func (module IndexDiffModule) Setup(cfg *config.Config) {

	ok, err := env.ParseConfig("index_diff", &diffConfig)
	if ok && err != nil {
		panic(err)
	}

	testChan = CompareChan{
		msgChans: map[string]chan CompareItem{},
		stopChan: make(chan struct{}),
	}

	testChan.msgChans[diffConfig.GetSortedLeftQueue()] = make(chan CompareItem, diffConfig.BufferSize)
	testChan.msgChans[diffConfig.GetSortedRightQueue()] = make(chan CompareItem, diffConfig.BufferSize)

}

func (module IndexDiffModule) Start() error {

	if !diffConfig.Enabled {
		return nil
	}

	go func() {
		defer func() {
			if !global.Env().IsDebug {
				if r := recover(); r != nil {
					var v string
					switch r.(type) {
					case error:
						v = r.(error).Error()
					case runtime.Error:
						v = r.(runtime.Error).Error()
					case string:
						v = r.(string)
					}
					log.Error("error in index_diff service", v)
				}
			}
		}()

		queueNames := []string{diffConfig.SourceInputQueue, diffConfig.TargetInputQueue}

		for _, q := range queueNames {
			wg.Add(1)
			go func(q string) {
				defer wg.Done()
				buffer := bytebufferpool.Get()
				//build sorted file
				sorter := extsort.New(nil)
				file := path.Join(global.Env().GetDataDir(), "diff", q)
				sortedFile := path.Join(global.Env().GetDataDir(), "diff", q+"_sorted")

				if !util.FileExists(sortedFile) {
					err := util.FileLinesWalk(file, func(bytes []byte) {
						_ = sorter.Append(bytes)
					})
					if err != nil {
						panic(err)
					}

					defer sorter.Close()
					// Sort and iterate.
					iter, err := sorter.Sort()
					if err != nil {
						panic(err)
					}
					defer iter.Close()

					for iter.Next() {
						buffer.Write(iter.Data())
						buffer.WriteByte('\n')

						if buffer.Len() > 10*1024 {
							util.FileAppendContentWithByte(sortedFile, buffer.Bytes())
							buffer.Reset()
						}
					}

					util.FileAppendContentWithByte(sortedFile, buffer.Bytes())
					bytebufferpool.Put(buffer)
					if err := iter.Err(); err != nil {
						panic(err)
					}
				} else {
					log.Debugf("sorted file exists, ignore,", sortedFile)
				}

				//popup sorted list
				err := util.FileLinesWalk(sortedFile, func(bytes []byte) {
					arr := strings.FieldsFunc(string(bytes), func(r rune) bool {
						return r == ','
					})
					if len(arr) != 2 {
						log.Error("invalid line:", util.UnsafeBytesToString)
						return
					}
					id := arr[0]
					hash := arr[1]
					item := CompareItem{
						//Doc:  doc,
						Key:  id,
						Hash: hash,
					}
					testChan.msgChans[q+"_sorted"] <- item
				})
				if err != nil {
					panic(err)
				}

			}(q)
		}

		go processMsg(func(result DiffResult) {
			queue.Push(diffConfig.DiffQueue, util.MustToJSONBytes(result))
		})

		wg.Wait()

		if diffConfig.TextReportEnabled {
			go func() {
				path1 := path.Join(global.Env().GetLogDir(), "diff_result", fmt.Sprintf("%v_vs_%v", diffConfig.SourceInputQueue, diffConfig.TargetInputQueue))
				os.MkdirAll(path1, 0775)
				file := path.Join(path1, util.FormatTimeForFileName(time.Now())+".log")
				str := "    ___ _  __  __     __                 _ _   \n"
				str += "   /   (_)/ _|/ _|   /__\\ ___  ___ _   _| | |_ \n"
				str += "  / /\\ / | |_| |_   / \\/// _ \\/ __| | | | | __|\n"
				str += " / /_//| |  _|  _| / _  \\  __/\\__ \\ |_| | | |_ \n"
				str += "/___,' |_|_| |_|   \\/ \\_/\\___||___/\\__,_|_|\\__|\n"

				strBuilder := strings.Builder{}
				leftBuilder := strings.Builder{}
				rightBuilder := strings.Builder{}
				bothBuilder := strings.Builder{}
				strBuilder.WriteString(str)

				var i, left, right, both int

			WAIT:
				timeOut := 5 * time.Second
				for {

					v, timeout, err := queue.PopTimeout(diffConfig.DiffQueue, timeOut)
					if timeout {

						if len(testChan.msgChans[diffConfig.GetSortedLeftQueue()]) > 0 ||
							len(testChan.msgChans[diffConfig.GetSortedRightQueue()]) > 0 {
							time.Sleep(5 * time.Second)
							log.Debug("waiting for:", len(testChan.msgChans[diffConfig.GetSortedLeftQueue()]), ",", len(testChan.msgChans[diffConfig.GetSortedRightQueue()]))
							goto WAIT
						}
						goto RESULT
					}

					i++
					doc := DiffResult{}
					err = util.FromJSONBytes(v, &doc)
					if err != nil {
						log.Error(err)
						return
					}
					docID := ""
					docHash := ""
					if doc.Source != nil {
						docID = doc.Source.Key
						docHash = doc.Source.Hash
					}
					if doc.Target != nil {
						docID = doc.Target.Key
						docHash = doc.Target.Hash
					}

					switch doc.DiffType {
					case "OnlyInSource":
						left++
						leftBuilder.WriteString(fmt.Sprintf("doc:%v, hash:%v\n", docID, docHash))
						break
					case "OnlyInTarget":
						right++
						rightBuilder.WriteString(fmt.Sprintf("doc:%v, hash:%v\n", docID, docHash))
						break
					case "DiffBoth":
						both++
						bothBuilder.WriteString(fmt.Sprintf("doc:%v, hash:%v vs %v\n", docID, doc.Source.Hash, doc.Target.Hash))
						break
					}
				}
			RESULT:
				fmt.Println(strBuilder.String())
				util.FileAppendNewLine(file, strBuilder.String())

				if leftBuilder.Len() > 0 {
					str := fmt.Sprintf("%v documents only exists in left side:", left)
					fmt.Println(str)
					fmt.Println(leftBuilder.String())

					util.FileAppendNewLine(file, str)
					util.FileAppendNewLine(file, leftBuilder.String())
				}
				if rightBuilder.Len() > 0 {

					str := fmt.Sprintf("%v documents only exists in right side:", right)
					fmt.Println(str)
					fmt.Println(rightBuilder.String())

					util.FileAppendNewLine(file, str)
					util.FileAppendNewLine(file, rightBuilder.String())
				}
				if bothBuilder.Len() > 0 {

					str := fmt.Sprintf("%v documents exists but diff in both side:", both)
					fmt.Println(str)
					fmt.Println(bothBuilder.String())

					util.FileAppendNewLine(file, str)
					util.FileAppendNewLine(file, bothBuilder.String())
				}

				log.Infof("diff finished.")
			}()
		}

		wg.Add(1)
		wg.Wait()

	}()

	return nil
}

func (module IndexDiffModule) Stop() error {
	if !diffConfig.Enabled {
		return nil
	}
	close(testChan.stopChan)
	wg.Done()
	return nil
}
