package content

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"

	"github.com/jfrog/jfrog-client-go/utils"
	"github.com/jfrog/jfrog-client-go/utils/errorutils"
)

type ContentReader struct {
	// Response data file path.
	filePath, arrayKey string
	// The objects from the response data file are being pushed to the data channel.
	dataChannel chan map[string]interface{}
	buffer      []map[string]interface{}
	errorsQueue *utils.ErrorsQueue
	once        *sync.Once
}

func NewContentReader(filePath string, arrayKey string) *ContentReader {
	self := ContentReader{}
	self.filePath = filePath
	self.arrayKey = arrayKey
	self.dataChannel = make(chan map[string]interface{}, 50000)
	self.errorsQueue = utils.NewErrorsQueue(50000)
	self.once = new(sync.Once)
	return &self
}

func (rr *ContentReader) ArrayKey(arrayKey string) *ContentReader {
	rr.arrayKey = arrayKey
	return rr
}

func (rr *ContentReader) NextRecord(recordOutput interface{}) error {
	rr.once.Do(func() {
		go func() {
			rr.run()
		}()
	})
	record, ok := <-rr.dataChannel
	if !ok {
		return errorutils.CheckError(io.EOF)
	}
	data, _ := json.Marshal(record)
	return errorutils.CheckError(json.Unmarshal(data, recordOutput))
}

func (rr *ContentReader) Reset() {
	rr.dataChannel = make(chan map[string]interface{}, 50000)
	rr.once = new(sync.Once)
}

func (rr *ContentReader) Close() error {
	if rr.filePath != "" {
		return errorutils.CheckError(os.Remove(rr.filePath))
	}
	return nil
}

func (rr *ContentReader) GetFilePath() string {
	return rr.filePath
}

func (rr *ContentReader) SetFilePath(newPath string) {
	if rr.filePath != "" {
		rr.Close()
	}
	rr.filePath = newPath
	rr.dataChannel = make(chan map[string]interface{}, 2)
}

// Fill the channel from local file path
func (rr *ContentReader) run() {
	fd, err := os.Open(rr.filePath)
	if err != nil {
		log.Fatal(err.Error())
		rr.errorsQueue.AddError(errorutils.CheckError(err))
		return
	}
	br := bufio.NewReaderSize(fd, 65536)
	defer fd.Close()
	defer close(rr.dataChannel)
	dec := json.NewDecoder(br)
	err = findDecoderTargetPosition(dec, rr.arrayKey, true)
	if err != nil {
		if err == io.EOF {
			rr.errorsQueue.AddError(errors.New("results not found"))
			return
		}
		rr.errorsQueue.AddError(err)
		log.Fatal(err.Error())
		return
	}
	for dec.More() {
		var ResultItem map[string]interface{}
		err := dec.Decode(&ResultItem)
		if err != nil {
			log.Fatal(err)
			rr.errorsQueue.AddError(errorutils.CheckError(err))
			return
		}
		rr.dataChannel <- ResultItem
	}
}

func (rr *ContentReader) IsEmpty() (bool, error) {
	fd, err := os.Open(rr.filePath)
	if err != nil {
		log.Fatal(err.Error())
		rr.errorsQueue.AddError(errorutils.CheckError(err))
		return false, err
	}
	br := bufio.NewReaderSize(fd, 65536)
	defer fd.Close()
	defer close(rr.dataChannel)
	dec := json.NewDecoder(br)
	err = findDecoderTargetPosition(dec, rr.arrayKey, true)
	return isZeroResult(dec, rr.arrayKey, true)
}

func (rr *ContentReader) GetError() error {
	return rr.errorsQueue.GetError()
}

func findDecoderTargetPosition(dec *json.Decoder, target string, isArray bool) error {
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return errorutils.CheckError(err)
		}
		if t == target {
			if isArray {
				// Skip '['
				_, err = dec.Token()
			}
			return errorutils.CheckError(err)
		}
	}
	return nil
}

func isZeroResult(dec *json.Decoder, target string, isArray bool) (bool, error) {
	if err := findDecoderTargetPosition(dec, target, isArray); err != nil {
		return false, err
	}
	t, err := dec.Token()
	if err != nil {
		return false, errorutils.CheckError(err)
	}
	return t == json.Delim('{'), nil
}
