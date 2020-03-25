package main

import (
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/geziyor/geziyor"
	"github.com/geziyor/geziyor/client"
	"github.com/jasonlvhit/gocron"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type TrackerDetails struct {
	Email string
	Site  string
	Link  string
	Error *Error
	GG    bool
	HB    bool
	N11   bool
	Price float32
}

type Error struct {
	Empty         bool
	EmailNotValid bool
	LinkNotValid  bool
	NoMatch       bool
	NoSuchLink    bool
}

type DB struct {
	Items []Item `json:"items"`
}

type Item struct {
	Site        string   `json:"site"`
	Link        string   `json:"link"`
	Subscribers []string `json:"subscribers"`
	Price       float32  `json:"price"`
}

var (
	details          TrackerDetails
	subscribe_form   *template.Template
	info             *template.Template
	unsubscribe_form *template.Template
	data             DB
)

func main() {
	subscribe_form = template.Must(template.ParseFiles("pages/forms.html"))
	info = template.Must(template.ParseFiles("pages/info.html"))
	unsubscribe_form = template.Must(template.ParseFiles("pages/unsubscribe.html"))

	data = ReadJson()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			subscribe_form.Execute(w, nil)
			return
		}
		details = TrackerDetails{
			Email: r.FormValue("email"),
			Site:  r.FormValue("site"),
			Link:  r.FormValue("link"),
			Error: &Error{},
		}

		switch details.Site {
		case "gg":
			details.GG = true
			break
		case "hb":
			details.HB = true
			break
		case "n11":
			details.N11 = true
			break
		}
		if linkVal := ValidateLink(details.Link, details.Site); !linkVal {
			details.Error.LinkNotValid = true
		}

		if res := ValidateEmail(details.Email); !res {
			details.Error.EmailNotValid = true
		}

		if details.Site == "" || details.Email == "" || details.Link == "" {
			details.Error.Empty = true
		}

		if details.Error.LinkNotValid || details.Error.Empty || details.Error.EmailNotValid {
			subscribe_form.Execute(w, details)
			return
		}
		AddSubscription(details.Link, details.Email)
		info.Execute(w, details)

	})

	http.HandleFunc("/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			unsubscribe_form.Execute(w, nil)
			return
		}
		details = TrackerDetails{
			Email: r.FormValue("email"),
			Link:  r.FormValue("link"),
			Site:  GetSiteFromLink(r.FormValue("link")),
			Error: &Error{},
		}
		if linkVal := ValidateLink(details.Link, details.Site); !linkVal {
			details.Error.LinkNotValid = true
		}

		if res := ValidateEmail(details.Email); !res {
			details.Error.EmailNotValid = true
		}

		if details.Site == "" || details.Email == "" || details.Link == "" {
			details.Error.Empty = true
		}
		res := Unsubscribe(details.Link, details.Email)

		if res == "noLink" {
			details.Error.NoSuchLink = true
		} else if res == "noUser" {
			details.Error.NoMatch = true
		}

		if details.Error.LinkNotValid || details.Error.Empty || details.Error.EmailNotValid || details.Error.NoMatch || details.Error.NoSuchLink {
			unsubscribe_form.Execute(w, details)
			return
		}

		info.Execute(w, details)
	})

	go executeGoCronJob()

	var port = os.Getenv("PORT")

	if port == "" {
		port = "8080"
		log.Print("No port, set to default 8080")
	}

	port = ":" + port
	fmt.Println("listening on port " + port)
	log.Fatal(http.ListenAndServe(port, nil))

}

// run task every hour
func executeGoCronJob() {
	gocron.Every(1).Hour().Do(Task)
	<-gocron.Start()
}

// checks if the given email is valid
func ValidateEmail(email string) bool {
	res, _ := regexp.Match("[A-Z0-9a-z._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,64}", []byte(email))
	return res
}

// checks if the given link is valid
func ValidateLink(link string, site string) bool {
	var res bool
	switch site {
	case "gg":
		res, _ = regexp.Match(`^(?:http(s)?://)?[\w.-]+(?:\.[\\bgittigidiyor\\b\.-]+)+[\w\-\._~:/?#[\]@!\$&'\(\)\*\+,;=.]+$`, []byte(link))
		break
	case "hb":
		res, _ = regexp.Match(`^(?:http(s)?://)?[\w.-]+(?:\.[\\bhepsiburada\\b\.-]+)+[\w\-\._~:/?#[\]@!\$&'\(\)\*\+,;=.]+$`, []byte(link))
		break
	case "n11":
		res, _ = regexp.Match(`^(?:http(s)?://)?[urun.-]+(?:\.[\\bn11\\b\.-]+)+[\w\-\._~:/?#[\]@!\$&'\(\)\*\+,;=.]+$`, []byte(link))
		break
	}
	return res
}

// crawl function for sites that use javascript functions
func GetDynamicPrice(link string) string {
	var price string
	geziyor.NewGeziyor(&geziyor.Options{
		StartRequestsFunc: func(g *geziyor.Geziyor) {
			g.GetRendered(link, g.Opt.ParseFunc)
		},
		ParseFunc: func(g *geziyor.Geziyor, r *client.Response) {
			price = r.HTMLDoc.Find("div.extra-discount-price").Text()
		},
		//BrowserEndpoint: "ws://localhost:3000",
	}).Start()
	return price
}

// crawl function fot static pages
func GetPrice(link string, site string) *goquery.Selection {
	var element string
	resp, err := http.Get(link)
	if err != nil {
		return nil
	}

	if site == "gg" {
		element = "div#sp-price-lowPrice"
	} else {
		element = "div.newPrice"
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}
	price := doc.Find(element)
	return price
}

// returns title of the price
func GetTitle(link string) string {
	resp, err := http.Get(link)
	if err != nil {
		log.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	title := doc.Find("title").Text()
	return title
}

// parses string to float
func ParsePrice(str string) float32 {
	array := strings.Split(str, " ")
	array[0] = strings.Replace(array[0], ",", ".", -1)
	if strings.Count(array[0], ".") > 1 {
		array[0] = strings.Replace(array[0], ".", "", 1)
	}
	price, _ := strconv.ParseFloat(array[0], 32)
	return float32(price)
}

// add new record to json
func AddSubscription(link string, email string) {
	data = ReadJson()
	for i := 0; i < len(data.Items); i++ {
		if data.Items[i].Link == link {
			println(data.Items[i].Link)
			for j := 0; j < len(data.Items[i].Subscribers); j++ {
				println(data.Items[i].Subscribers[j])
				if data.Items[i].Subscribers[j] == email {
					return
				}
			}
			data.Items[i].Subscribers = append(data.Items[i].Subscribers, email)
			WriteJson(data)
			return
		}
	}
	CrawlNew()
	AddItemWithSub(data, GetSiteFromLink(link), link, email)
}

// add new item with a subscriber
func AddItemWithSub(data DB, site string, link string, email string) {
	item := Item{
		Site:        site,
		Link:        link,
		Subscribers: []string{email},
		Price:       details.Price,
	}
	data.Items = append(data.Items, item)
	WriteJson(data)
	data = ReadJson()
}

// load json
func ReadJson() DB {
	file, _ := ioutil.ReadFile("db.json")
	_ = json.Unmarshal(file, &data)
	return data
}

// save json
func WriteJson(newData DB) {
	result, err := json.Marshal(newData)
	if err != nil {
		log.Fatal(err)
	}
	err = ioutil.WriteFile("db.json", result, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

// crawls the page in the data object
func CrawlNew() {
	if details.HB {
		details.Price = ParsePrice(GetDynamicPrice(details.Link))
	} else {
		GetPrice(details.Link, details.Site).Each(func(_ int, selection *goquery.Selection) {
			details.Price = ParsePrice(strings.TrimSpace(selection.Text()))
		})
	}
}

// crawls the given input
func CrawlWithInput(link string) float32 {
	var price float32
	site := GetSiteFromLink(link)
	if site == "hb" {
		price = ParsePrice(GetDynamicPrice(link))
	} else {
		GetPrice(link, GetSiteFromLink(link)).Each(func(_ int, selection *goquery.Selection) {
			price = ParsePrice(strings.TrimSpace(selection.Text()))
		})
	}
	return price
}

// return the site info from the given input
func GetSiteFromLink(link string) string {
	var site string
	if strings.Contains(link, "urun.n11") {
		site = "n11"
	} else if strings.Contains(link, "hepsiburada") {
		site = "hb"
	} else {
		site = "gg"
	}

	return site
}

// the task to run every 1 hour, crawls every item and compares old and new prices, sends email if price is changed
func Task() {
	for i := 0; i < len(data.Items); i++ {
		oldPrice := data.Items[i].Price
		newPrice := CrawlWithInput(data.Items[i].Link)
		if newPrice < oldPrice {
			fmt.Printf("%s ha bunun fiyatı %f dan %f buna düşmüştür \n", GetTitle(data.Items[i].Link), oldPrice, newPrice)
			for j := 0; j < len(data.Items[i].Subscribers); j++ {

				SendMail(data.Items[i].Subscribers[j], fmt.Sprintf("%s isimli ürünün fiyatı artık: %f (eski fiyatı: %f)\nÜrünün linki: %s", GetTitle(data.Items[i].Link), newPrice, oldPrice, data.Items[i].Link), "Takip ettiğiniz bir ürünün fiyatı değişti!")
			}

			data.Items[i].Price = newPrice
			WriteJson(data)
		}

	}
}

// sends email with smtp
func SendMail(email string, message string, subject string) {

	username := "enter your mailbot email"
	password := "enter your mailbot password"

	auth := smtp.PlainAuth(
		"",
		username,
		password,
		"smtp.gmail.com",
	)

	mail := "From: " + username + "\n" +
		"To: " + email + "\n" +
		"Subject: " + subject + "\n\n" +
		message

	err := smtp.SendMail(
		"smtp.gmail.com:587", //set for gmail
		auth,
		username,
		[]string{email},
		[]byte(mail),
	)
	if err != nil {
		log.Fatal(err)
	}
}

// deletes user from given item
func Unsubscribe(link string, email string) string {
	var new_sub_list []string
	data := ReadJson()
	for i := 0; i < len(data.Items); i++ {
		if data.Items[i].Link == link {
			for j := 0; j < len(data.Items[i].Subscribers); j++ {
				current_mail := data.Items[i].Subscribers[j]
				if current_mail != email {
					new_sub_list = append(new_sub_list, current_mail)
				}
			}
			if len(data.Items[i].Subscribers) == len(new_sub_list) {
				return "noUser"
			}
			if len(new_sub_list) < 1 {
				RemoveItem(link)
			} else {
				data.Items[i].Subscribers = new_sub_list
				WriteJson(data)
			}

			return "successful"
		}

	}
	return "noLink"
}

// deletes item from json
func RemoveItem(link string) {
	newData := DB{}
	data := ReadJson()
	for i := 0; i < len(data.Items); i++ {
		if data.Items[i].Link != link {
			newData.Items = append(newData.Items, data.Items[i])
		}
	}
	WriteJson(newData)
}
