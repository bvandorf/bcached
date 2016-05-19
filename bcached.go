package main

import (
	"fmt"
	"net/http"
	"strings"
	"strconv"
	"encoding/json"
	"time"
	"bytes"
	"errors"
	"os"
	"flag"
	"io/ioutil"
)

const VERSION = "1.0"
var debug = false

var memcmd = make(chan memListCmd)
var memcmdr = make(chan []byte)
var memcmder = make(chan error)

type ServerOption struct {
	port int
	dir string
}

var options = ServerOption{}

var usageStr = `
Usage: bcached [options]
Server Options:
    -p, --port <port>                port for clients (default: 8080)
    -d, --dir <folder>               directory to save cache (default: ./data)

Common Options:
    -h, --help                       Show this message
    -v, --version                    Show version
    --debug                          Show debug info
	
Client Usage: 
POST { 'Key':'<key>' } 
	-> http://localhost:8080/clinet/get
	
POST { 'Key':'<key>', 'FromValue':'<oldvalue>', 'Value':'<newvalue>' } 
	-> http://localhost:8080/clinet/put
`

// usage will print out the flag options for the server.
func usage() {
	fmt.Printf("%s\n", usageStr)
	os.Exit(0)
}

func main() {
	var showHelp = false
	flag.BoolVar(&showHelp, "h", false, "")
	flag.BoolVar(&showHelp, "help", false, "")
	
	var showVersion = false
	flag.BoolVar(&showVersion, "v", false, "")
	flag.BoolVar(&showVersion, "version", false, "")
	
	
	flag.IntVar(&options.port, "p", 8080, "")
	flag.IntVar(&options.port, "port", 8080, "")
	
	flag.StringVar(&options.dir, "d", "./data", "")
	flag.StringVar(&options.dir, "dir", "./data", "")
	
	flag.BoolVar(&debug, "debug", false, "")
	
	flag.Usage = usage

	flag.Parse()
	
//	if (len(flag.Args()) == 0) {
//		showHelp = true
//	}

	// Show version and exit
	if showVersion {
		fmt.Printf("version %s\n", VERSION)
		os.Exit(-1)
	}

	if showHelp {
		usage()
		os.Exit(-1)
	}




	//start memcach
	go mem(memcmd, memcmdr, memcmder)
	
	//start client web
	go ClientWebAPI(options.port)
	
	
	fmt.Println("Hit Enter To Exit")
	fmt.Scanln()
}

type memListCmd struct {
	cmdType string
	cmdKey string
	cmdVal []byte
	cmdFromVal []byte
}

type memListData struct {
	Key string
	Value []byte
	LastWrite time.Time
}

var memList = make(map[string][]byte)
func mem(cmd chan memListCmd, val chan []byte, er chan error) {
	//check if data dir exists
	err := MakeDir(options.dir)
	if (err != nil && err.Error() != "Path Exists") {
		fmt.Println("Error: " + err.Error())
		panic(err)
	}
	
	for {
		c := <- cmd
		if (c.cmdType == "get") {
			v,ok := memList[c.cmdKey]
			if (ok == true) {
				val <- v
				continue
			}
			
			b,err := ReadByteSliceOfFile(options.dir + "/" + c.cmdKey + ".json")
			if (err != nil) {
				er <- err
				continue
			}
			
			jsd := memListData{}
			err = json.Unmarshal(b, &jsd)
			if (err != nil) {
				er <- err
				continue
			}
			
			memList[c.cmdKey] = jsd.Value
			val <- jsd.Value
		}
		
		if (c.cmdType == "put") {
			if (len(c.cmdFromVal) != 0 && bytes.Equal(memList[c.cmdKey], c.cmdFromVal) == false) {
				er <- errors.New("From Value Does Not Match")
				continue
			}
			
			memList[c.cmdKey] = c.cmdVal
			
			jsd := memListData{c.cmdKey, c.cmdVal, time.Now()}
			b,err := json.Marshal(&jsd)
			if (err != nil) {
				er <- err
				continue
			}
			
			err = WriteByteSliceToFile(options.dir + "/" + c.cmdKey + ".json", b)
			if (err != nil) {
				er <- err
				continue
			}
			
			val <- []byte{}
		}
	}
	
	return
}

func getFromMem(key string) ([]byte, error) {
	memcmd <- memListCmd{"get", key, []byte{}, []byte{}}
	select {
		case v := <- memcmdr:
			return v, nil
		case e := <- memcmder:
			return []byte{}, e
	}
	
	return []byte{}, errors.New("idk")
}

func putOnMem(key string, val, fromval []byte) (error) {
	memcmd <- memListCmd{"put", key, val, fromval}
	select {
		case <- memcmdr:
			return nil
		case e := <- memcmder:
			return e
	}
	
	return errors.New("idk")
}

type clientJSON struct {
	Key string
	Value string
	FromValue string
}


func ClientWebAPI(port int) {
	http.HandleFunc("/client/", func(w http.ResponseWriter, r *http.Request) { 
		//clinet api
		
		split := strings.Split(r.URL.Path, "/")
		urlkey := split[len(split)-1]
		
		if (urlkey == "get") {
			js := clientJSON{}
			err := json.NewDecoder(r.Body).Decode(&js)
			if (err != nil) {
				if (debug) {
					fmt.Println("JSON Error:" + err.Error())
				}
				http.Error(w, "JSON Error", http.StatusInternalServerError)
				return
			}
			
			key := js.Key
			
			val, err := getFromMem(key)
			if (err != nil) {
				if (debug) {
					fmt.Println("Get Error:" + err.Error())
				}
				http.Error(w, "Get Error", http.StatusInternalServerError)
				return
			}
			
			js.Value = string(val)
			
			err = json.NewEncoder(w).Encode(js)
			if (err != nil) {
				if (debug) {
					fmt.Println("JSON Encode Error:" + err.Error())
				}
				http.Error(w, "JSON Encode Error", http.StatusInternalServerError)
				return
			}
			
			if (debug) {
				fmt.Println("GET:" + key + "=", val)
			}
			
			return
		}
		
		if (urlkey == "put") {
			js := clientJSON{}
			err := json.NewDecoder(r.Body).Decode(&js)
			if (err != nil) {
				if (debug) {
					fmt.Println("JSON Error:" + err.Error())
				}
				http.Error(w, "JSON Error", http.StatusInternalServerError)
				return
			}
			
			key := js.Key
			val := []byte(js.Value)
			fval := []byte(js.FromValue)
			
			err = putOnMem(key, val, fval)
			if (err != nil) {
				if (debug) {
					fmt.Println("Put Error:" + err.Error())
				}
				http.Error(w, "Put Error", http.StatusInternalServerError)
				return
			}
			
			if (debug) {
				fmt.Println("PUT:" + key + "=", val)
			}
			
			return
		}
		
		http.Error(w, "Not Found", http.StatusInternalServerError)
		fmt.Println("Path Not Fond For Link:" + r.URL.Path)
	})
	
	fmt.Println("Web Listening On Port ", port)
	err := http.ListenAndServe(":" + strconv.Itoa(port), nil)
	if (err != nil) {
		fmt.Println("Error Listening On Port " + strconv.Itoa(port) + ":" + err.Error())
	}
}

func MakeDir(path string) error {
	b, err := FolderExists(path)
	if err != nil {
		return err
	} else if b == true {
		return errors.New("Path Exists")
	} else {
		err := os.Mkdir(path, 0644)
		return err
	}
}

func FolderExists(path string) (bool, error) {
	f, err := os.Stat(path)
	if err == nil {
		if f.IsDir() == false {
			return true, errors.New("This Is A File")
		} else {
			return true, nil
		}
	} else {
		if os.IsNotExist(err) {
			return false, nil
		}
	}

	return true, err
}

func ReadByteSliceOfFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	b, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	} else {
		return b, nil
	}
}

func WriteByteSliceToFile(path string, data []byte) error {
	err := ioutil.WriteFile(path, data, 0644)
	return err
}

