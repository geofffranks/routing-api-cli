package routing_api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudfoundry-incubator/cf_http"
	"github.com/cloudfoundry-incubator/routing-api/db"
	"github.com/cloudfoundry-incubator/routing-api/trace"
	"github.com/tedsuo/rata"
	"github.com/vito/go-sse/sse"
	"net/http/httputil"
)

//go:generate counterfeiter -o fake_routing_api/fake_client.go . Client
type Client interface {
	SetToken(string)
	UpsertRoutes([]db.Route) error
	Routes() ([]db.Route, error)
	DeleteRoutes([]db.Route) error

	SubscribeToEvents() (EventSource, error)
}

func NewClient(url string) Client {
	return &client{
		httpClient:          cf_http.NewClient(),
		streamingHTTPClient: cf_http.NewStreamingClient(),

		reqGen: rata.NewRequestGenerator(url, Routes),
	}
}

type client struct {
	httpClient          *http.Client
	streamingHTTPClient *http.Client
	authToken           string

	reqGen *rata.RequestGenerator
}

func (c *client) SetToken(token string) {
	c.authToken = token
}

func (c *client) UpsertRoutes(routes []db.Route) error {
	return c.doRequest(UpsertRoute, nil, nil, routes, nil)
}

func (c *client) Routes() ([]db.Route, error) {
	var routes []db.Route
	err := c.doRequest(ListRoute, nil, nil, nil, &routes)
	return routes, err
}

func (c *client) DeleteRoutes(routes []db.Route) error {
	return c.doRequest(DeleteRoute, nil, nil, routes, nil)
}

func (c *client) SubscribeToEvents() (EventSource, error) {
	eventSource, err := sse.Connect(c.streamingHTTPClient, time.Second, func() *http.Request {
		request, err := c.reqGen.CreateRequest(EventStreamRoute, nil, nil)
		request.Header.Add("Authorization", "bearer "+c.authToken)
		if err != nil {
			panic(err) // totally shouldn't happen
		}

		dumpRequest(request)
		return request
	})
	if err != nil {
		return nil, err
	}

	return NewEventSource(eventSource), nil
}

func (c *client) createRequest(requestName string, params rata.Params, queryParams url.Values, request interface{}) (*http.Request, error) {
	requestJson, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := c.reqGen.CreateRequest(requestName, params, bytes.NewReader(requestJson))
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = queryParams.Encode()
	req.ContentLength = int64(len(requestJson))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Authorization", "bearer "+c.authToken)

	return req, nil
}

func (c *client) doRequest(requestName string, params rata.Params, queryParams url.Values, request, response interface{}) error {
	req, err := c.createRequest(requestName, params, queryParams, request)
	if err != nil {
		return err
	}
	return c.do(req, response)
}

func (c *client) do(req *http.Request, response interface{}) error {
	dumpRequest(req)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	dumpResponse(res)

	if res.StatusCode > 299 {
		errResponse := Error{}
		json.NewDecoder(res.Body).Decode(&errResponse)
		return errResponse
	}

	if response != nil {
		return json.NewDecoder(res.Body).Decode(response)
	}

	return nil
}

func dumpRequest(req *http.Request) {
	dumpedRequest, err := httputil.DumpRequest(req, true)
	if err != nil {
		trace.Logger.Printf("Error dumping request\n%s\n", err)
	} else {
		trace.Logger.Printf("\n%s [%s]\n%s\n", "REQUEST:", time.Now().Format(time.RFC3339), trace.Sanitize(string(dumpedRequest)))
	}
}

func dumpResponse(resp *http.Response) {
	dumpedResponse, err := httputil.DumpResponse(resp, true)
	if err != nil {
		trace.Logger.Printf("Error dumping response\n%s\n", err)
	} else {
		trace.Logger.Printf("\n%s [%s]\n%s\n", "RESPONSE:", time.Now().Format(time.RFC3339), trace.Sanitize(string(dumpedResponse)))
	}
}
