package maptiles

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	log "github.com/Sirupsen/logrus"
)

var (
	tsv *TileServer
	// once used to make sure groupcache only assigned once,
	// see https://github.com/golang/groupcache/blob/master/groupcache_test.go
	once        sync.Once
	urltemplate string
	cachedir    = "./"
)

func init() {
	// log.SetFormatter(&log.JSONFormatter{})
	f := &log.TextFormatter{}
	f.FullTimestamp = true
	log.SetFormatter(f)
	log.SetLevel(log.WarnLevel)
}

func tileserverSetup() {
	tsv = NewTileServer(urltemplate, cachedir)
}

// TestServerError test tile server source for Server Error.
func TestServerError(t *testing.T) {
	serverError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "500 page not found", http.StatusInternalServerError)
	}))
	urltemplate = serverError.URL
	defer serverError.Close()
	log.Printf("Mock Mapbox Studio: %s", urltemplate)

	once.Do(tileserverSetup)
	ts := httptest.NewServer(tsv)
	defer ts.Close()

	mapproxy_url := ts.URL + "/street/1/0/0.png"
	log.Printf("Mock go-mapproxy: %s\n", mapproxy_url)
	res, err := http.Get(mapproxy_url)
	if err != nil {
		log.Fatal(err)
	}
	log.Info("StatusCode: ", res.StatusCode)
	// Should be 500, but for now go-mapnik assume any TileFetchResult.Blob==nil is 404
	if res.StatusCode != 404 {
		t.Errorf("StatusCode: got %d; want %d", res.StatusCode, 404)
	}
	content, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Content length: ", len(content))
	log.Info("Content: ", string(content))
}

// TestNotFound test tile server source for 404 response.
func TestNotFound(t *testing.T) {

	serverError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	urltemplate = serverError.URL
	defer serverError.Close()
	log.Printf("Mock Mapbox Studio: %s", urltemplate)

	once.Do(tileserverSetup)
	ts := httptest.NewServer(tsv)
	defer ts.Close()

	mapproxy_url := ts.URL + "/street/1/0/0.png"
	log.Printf("Mock go-mapproxy: %s\n", mapproxy_url)
	res, err := http.Get(mapproxy_url)
	if err != nil {
		log.Fatal(err)
	}
	log.Info("StatusCode: ", res.StatusCode)
	if res.StatusCode != 404 {
		t.Errorf("StatusCode: got %d; want %d", res.StatusCode, 404)
	}
	content, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Content length: ", len(content))
	log.Info("Content: ", string(content))
}
