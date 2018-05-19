/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sync"
	//"time"
)

const (
	readTimeout = 1000
	keyLen      = 64
)

type proxy struct {
	C      chan proxyPacket
	key    string
	conn   net.Conn
	buf    *bytes.Buffer
	bufmux *sync.Mutex
}

type proxyPacket struct {
	c    http.ResponseWriter
	r    *http.Request
	done chan bool
}

func NewProxy(key, destAddr string) (p *proxy, err error) {
	log.Println("Attempting connect", destAddr)
	conn, err := net.Dial("tcp", destAddr)
	p = newproxy(conn, key)
	if err != nil {
		return
	}
	//p.conn.SetReadDeadline(time.Now().Add(time.Millisecond * readTimeout))
	log.Println("ResponseWriterected", destAddr)
	return
}

func newproxy(conn net.Conn, key string) *proxy {
	var p = new(proxy)
	p.C = make(chan proxyPacket)
	p.key = key
	p.conn = conn
	p.buf = new(bytes.Buffer)
	p.bufmux = new(sync.Mutex)
	go func() {
		for {
			b := make([]byte, 1024)
			n, _ := p.conn.Read(b)
			if n > 0 {
				b = b[:n]
				p.bufmux.Lock()
				p.buf.Write(b)
				p.bufmux.Unlock()
			}
		}
	}()
	return p
}

func (p *proxy) handle(pp proxyPacket) {
	// read from the request body and write to the ResponseWriter
	//_, err := io.Copy(p.conn, pp.r.Body)
	b := make([]byte, 65536)
	n, _ := pp.r.Body.Read(b)
	if n > 0 {
		b = b[:n]
		//log.Println("http in: ", b)
		p.conn.Write(b)
	}
	pp.r.Body.Close()
	pp.c.Header().Set("Content-type", "application/octet-stream")

	res := bytes.NewBuffer([]byte(p.key))
	p.bufmux.Lock()
	//log.Println("http out: ", p.buf.Bytes())
	p.buf.WriteTo(res)
	p.bufmux.Unlock()
	io.Copy(pp.c, res)

	pp.done <- true
}

var queue = make(chan proxyPacket)
var createQueue = make(chan *proxy)

func handler(c http.ResponseWriter, r *http.Request) {
	pp := proxyPacket{c, r, make(chan bool)}
	queue <- pp
	<-pp.done // wait until done before returning
}

func createHandler(c http.ResponseWriter, r *http.Request) {
	// read destAddr
	destAddr, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(c, "Could not read destAddr",
			http.StatusInternalServerError)
		return
	}

	key := genKey()

	p, err := NewProxy(key, string(destAddr))
	if err != nil {
		http.Error(c, "Could not connect",
			http.StatusInternalServerError)
		return
	}
	createQueue <- p
	c.Write([]byte(key))
}

func proxyMuxer() {
	proxyMap := make(map[string]*proxy)
	for {
		select {
		case pp := <-queue:
			key := make([]byte, keyLen)
			// read key
			n, err := pp.r.Body.Read(key)
			if n != keyLen || (err != nil && err != io.EOF) {
				log.Println("Couldn't read key", key)
				continue
			}
			// find proxy
			p, ok := proxyMap[string(key)]
			if !ok {
				log.Println("Couldn't find proxy", key)
				continue
			}
			// handle
			p.handle(pp)
		case p := <-createQueue:
			proxyMap[p.key] = p
		}
	}
}

var httpAddr = flag.String("http", ":8080", "http listen address")

func main() {
	flag.Parse()

	go proxyMuxer()

	http.HandleFunc("/", handler)
	http.HandleFunc("/create", createHandler)
	http.ListenAndServe(*httpAddr, nil)
}

func genKey() string {
	key := make([]byte, keyLen)
	for i := 0; i < keyLen; i++ {
		key[i] = byte(rand.Int())
	}
	return string(key)
}
