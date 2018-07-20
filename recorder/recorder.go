package recorder

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"time"

	"github.com/happal/monsoon/request"
	"github.com/happal/monsoon/response"
)

// Recorder records information about received responses in a file encoded as .
type Recorder struct {
	filename string
	*request.Request
	Template
	Extract     []string
	ExtractPipe []string
}

// Data is the data structure written to the file by a Recorder.
type Data struct {
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	TotalRequests  int       `json:"total_requests"`
	SentRequests   int       `json:"sent_requests"`
	HiddenRequests int       `json:"hidden_requests"`
	ShownRequests  int       `json:"shown_requests"`
	Cancelled      bool      `json:"cancelled"`

	Template    Template   `json:"template"`
	Responses   []Response `json:"responses"`
	Extract     []string   `json:"extract,omitempty"`
	ExtractPipe []string   `json:"extract_pipe,omitempty"`
}

// Response is the result of a request sent to the target.
type Response struct {
	Item  string `json:"item"`
	Error string `json:"error,omitempty"`

	StatusCode    int                `json:"status_code"`
	StatusText    string             `json:"status_text"`
	Header        response.TextStats `json:"header"`
	Body          response.TextStats `json:"body"`
	ExtractedData []string           `json:"extracted_data,omitempty"`
}

// New creates a new  recorder.
func New(filename string, request *request.Request, extract, extractPipe []string) (*Recorder, error) {
	t, err := NewTemplate(request)
	if err != nil {
		return nil, err
	}

	rec := &Recorder{
		filename:    filename,
		Request:     request,
		Template:    t,
		Extract:     extract,
		ExtractPipe: extractPipe,
	}
	return rec, nil
}

const statusInterval = time.Second

// Run reads responses from ch and forwards them to the returned channel,
// recording statistics on the way. When ch is closed or the context is
// cancelled, the output file is closed, processing stops, and the output
// channel is closed.
func (r *Recorder) Run(ctx context.Context, in <-chan response.Response, out chan<- response.Response, inCount <-chan int, outCount chan<- int) error {
	defer close(out)

	data := Data{
		Start:       time.Now(),
		End:         time.Now(),
		Template:    r.Template,
		Extract:     r.Extract,
		ExtractPipe: r.ExtractPipe,
	}

	lastStatus := time.Now()

	var countCh chan<- int // countCh is nil initially to disable sending

loop:
	for {
		var res response.Response
		var ok bool

		select {
		case <-ctx.Done():
			data.Cancelled = true
			break loop

		case res, ok = <-in:
			if !ok {
				// we're done, exit
				break loop
			}

		case total := <-inCount:
			data.TotalRequests = total
			// disable receiving on the in count channel
			inCount = nil
			// enable sending by setting countCh to outCount (which is not nil)
			countCh = outCount
			continue loop

		case countCh <- data.TotalRequests:
			// disable sending again by setting countCh to nil
			countCh = nil
			continue loop
		}

		data.SentRequests++
		if !res.Hide {
			data.ShownRequests++
			data.Responses = append(data.Responses, NewResponse(res))
		} else {
			data.HiddenRequests++
		}
		data.End = time.Now()

		if time.Since(lastStatus) > statusInterval {
			lastStatus = time.Now()

			err := r.dump(data)
			if err != nil {
				return err
			}
		}

		select {
		case <-ctx.Done():
			data.Cancelled = true
			break loop
		case out <- res:
		}
	}

	data.End = time.Now()
	return r.dump(data)
}

// dump writes the current status to the file.
func (r *Recorder) dump(data Data) error {
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	return ioutil.WriteFile(r.filename, buf, 0644)
}

// NewResponse builds a Response struct for serialization with JSON.
func NewResponse(r response.Response) (res Response) {
	res.Item = r.Item
	if r.Error != nil {
		res.Error = r.Error.Error()
	}

	if r.HTTPResponse != nil {
		res.StatusCode = r.HTTPResponse.StatusCode
		res.StatusText = r.HTTPResponse.Status
	}
	res.Header = r.Header
	res.Body = r.Body
	res.ExtractedData = r.Extract

	return res
}
