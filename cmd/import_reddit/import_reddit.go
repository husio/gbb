package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const commentsUrl = "https://www.reddit.com/r/%s/comments.json"

func main() {
	bbAddrFl := flag.String("bbaddr", "http://localhost:8000", "Address of BB")
	repeatFl := flag.Int("repeat", 1, "Repeat each entry")
	sectionFl := flag.String("section", "news", "Reddit that comments should be uploaded")
	flag.Parse()

	req, err := http.NewRequest("GET", fmt.Sprintf(commentsUrl, *sectionFl), nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("User-Agent", "hi")

	users := []*http.Client{
		registerUser(*bbAddrFl, "Andreas von Strucker"),
		registerUser(*bbAddrFl, "Antiphon the Overseer"),
		registerUser(*bbAddrFl, "Paul Norbert Ebersol"),
		registerUser(*bbAddrFl, "Jacqueline Falsworth"),
		registerUser(*bbAddrFl, "Trevor Fitzroy"),
		registerUser(*bbAddrFl, "Fortune, Dominic"),
		registerUser(*bbAddrFl, "Frankie and Victoria"),
		registerUser(*bbAddrFl, "Gabriel the Air-Walker"),
		registerUser(*bbAddrFl, "Georgianna Castleberry"),
		registerUser(*bbAddrFl, "Negasonic Teenage Warhead"),
		registerUser(*bbAddrFl, "Nicholas maunder"),
		registerUser(*bbAddrFl, "Red Claw"),
		registerUser(*bbAddrFl, "Nick Fury"),
		registerUser(*bbAddrFl, "Nighthawk"),
		registerUser(*bbAddrFl, "Ozymandias"),
		registerUser(*bbAddrFl, "Phantom Rider"),
		registerUser(*bbAddrFl, "Torgo the Vampire"),
		registerUser(*bbAddrFl, "Tuc"),
	}

	rand.Seed(time.Now().UnixNano())
	randomUser := func() *http.Client {
		return users[rand.Intn(len(users))]
	}

	resp, err := randomUser().Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Data struct {
			Children []struct {
				Data *RedditComment
			}
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		log.Fatal(err)
	}

	for repStep := 0; repStep < *repeatFl; repStep++ {
		// post as topics
		for _, c := range body.Data.Children {
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			if f, err := w.CreateFormField("subject"); err != nil {
				log.Fatal(err)
			} else {
				if _, err := io.WriteString(f, c.Data.LinkTitle); err != nil {
					log.Fatal(err)
				}
			}
			if f, err := w.CreateFormField("category"); err != nil {
				log.Fatal(err)
			} else {
				if _, err := io.WriteString(f, "1"); err != nil {
					log.Fatal(err)
				}
			}
			if f, err := w.CreateFormField("content"); err != nil {
				log.Fatal(err)
			} else {
				if _, err := io.WriteString(f, c.Data.Body); err != nil {
					log.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				log.Fatal(err)
			}

			req, err := http.NewRequest("POST", *bbAddrFl+"/t/new/", &b)
			if err != nil {
				log.Fatal(err)
			}
			req.Header.Set("Content-Type", w.FormDataContentType())

			if resp, err := randomUser().Do(req); err != nil {
				log.Fatal(err)
			} else {
				if resp.StatusCode > 299 {
					b, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 1e5))
					log.Fatalf("%d: %s", resp.StatusCode, b)
				}
				resp.Body.Close()
			}
		}

		// post as comments
		for _, c := range body.Data.Children {
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			if f, err := w.CreateFormField("content"); err != nil {
				log.Fatal(err)
			} else {
				if _, err := io.WriteString(f, c.Data.Body); err != nil {
					log.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				log.Fatal(err)
			}

			req, err := http.NewRequest("POST", *bbAddrFl+"/t/3/comment/", &b)
			if err != nil {
				log.Fatal(err)
			}
			req.Header.Set("Content-Type", w.FormDataContentType())

			if resp, err := randomUser().Do(req); err != nil {
				log.Fatal(err)
			} else {
				if resp.StatusCode > 299 {
					b, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 1e5))
					log.Fatalf("%d: %s", resp.StatusCode, b)
				}
				resp.Body.Close()
			}
		}
	}
}

type RedditComment struct {
	Body      string `json:"body"`
	LinkTitle string `json:"link_title"`
	Author    string `json:"author"`
}

func registerUser(addr string, name string) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{
		Jar: jar,
	}

	resp, err := client.PostForm(addr+"/register/", url.Values{
		"login":     []string{name},
		"password":  []string{"qwertyuiop"},
		"password2": []string{"qwertyuiop"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode > 299 {
		b, _ := ioutil.ReadAll(io.LimitReader(resp.Body, 1e5))
		log.Fatalf("%d: %s", resp.StatusCode, string(b))
	}
	return client
}
