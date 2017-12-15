package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dustin/go-humanize"
)

type (
	config struct {
		Token string `json:"token"`
	}

	tickerResponse struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Symbol      string `json:"symbol"`
		Rank        string `json:"rank"`
		PriceUSD    string `json:"price_usd"`
		PriceBTC    string `json:"price_btc"`
		MarketCap   string `json:"market_cap_usd"`
		Change1H    string `json:"percent_change_1h"`
		Change24H   string `json:"percent_change_24h"`
		Change7D    string `json:"percent_change_7d"`
		LastUpdated string `json:"last_updated"`
	}
)

const (
	tickerListEndpoint = "https://api.coinmarketcap.com/v1/ticker/?limit=0"
	tickerEndpoint     = "https://api.coinmarketcap.com/v1/ticker/"
)

var (
	tickers      = make([]tickerResponse, 0)
	rateLimit    = time.Second * 30
	updateRate   = time.Minute * 5
	lastMessages = make(map[string]time.Time)
	channels     = []string{"322882023825997845", "229807580367683584"}
)

func init() {
	for _, c := range channels {
		lastMessages[c] = time.Now()
	}
}

func main() {

	var configFile string
	if len(os.Args) == 2 {
		configFile = os.Args[1]
	} else {
		log.Fatalln("Please provide a configuration file as a second parameter")
	}

	file, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Printf("There was an error opening the file %s", configFile)
		log.Fatalln(err)
	}

	var config config
	json.Unmarshal(file, &config)

	bot, err := discordgo.New("Bot " + config.Token)
	if err != nil {
		log.Println("Error creating discord client")
		log.Fatalln(err)
	}

	bot.AddHandler(messageHandler)

	bot.Open()
	defer bot.Close()

	loadTickers()

	t := time.NewTimer(updateRate)
	go func() {
		for range t.C {
			loadTickers()
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT)
	<-sc
}

func loadTickers() {
	resp, err := http.Get(tickerListEndpoint)
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Tickers endpoint returned %d\n", resp.StatusCode)
	}

	var t []tickerResponse
	err = json.NewDecoder(resp.Body).Decode(&t)
	if err != nil {
		log.Fatalln(err)
	}

	if len(t) > 0 {
		tickers = t
		log.Printf("Loaded %d tickers\n", len(t))
	}
}

func getTicker(id string) (tickerResponse, error) {
	var tr tickerResponse
	resp, err := http.Get(fmt.Sprintf("%s%s", tickerEndpoint, id))
	if err != nil {
		log.Println(err)
		return tr, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Ticker endpoint returned %d\n", resp.StatusCode)
		return tr, errors.New("Bad status code")
	}

	var t []tickerResponse
	err = json.NewDecoder(resp.Body).Decode(&t)
	if err != nil {
		log.Println(err)
		return tr, err
	}

	if len(t) != 1 {
		return tr, errors.New("Bad response")
	}

	return t[0], nil
}

func messageHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isInSlice(m.ChannelID, channels) {
		return
	}

	msg := m.Message.Content
	msgSlice := strings.Split(msg, " ")
	if len(msgSlice) != 2 {
		return
	}

	if msgSlice[0] != "!c" && msgSlice[0] != "!crypto" {
		return
	}

	if time.Since(lastMessages[m.ChannelID]) < rateLimit {
		log.Println("Rate limited")
		return
	}

	ticker, found := findTicker(msgSlice[1])
	if found {
		ticker, err := getTicker(ticker.ID)
		if err == nil {
			sendTickerMessage(ticker, s, m)
		}
	}

}

func sendTickerMessage(t tickerResponse, s *discordgo.Session, m *discordgo.MessageCreate) {
	embed := makeEmbed(t)

	_, err := s.ChannelMessageSendEmbed(m.ChannelID, embed)

	if err != nil {
		log.Println("error sending message")
		log.Println(err)
	}

	lastMessages[m.ChannelID] = time.Now()
}

func makeEmbed(t tickerResponse) *discordgo.MessageEmbed {
	embed := discordgo.MessageEmbed{}

	embed.Title = "Coin Market Cap"
	embed.URL = "https://coinmarketcap.com/currencies/" + t.ID
	// embed.Color = 25520626

	// parsedStamp, err := strconv.ParseInt(t.LastUpdated, 10, 64)
	// if err == nil {
	// 	timestamp := unixToTime(parsedStamp)
	// 	embed.Timestamp = timestamp.Format("2017-12-14T23:26:52.599Z")
	// }

	embed.Author = &discordgo.MessageEmbedAuthor{
		Name:    fmt.Sprintf("%s (%s)", t.Name, t.Symbol),
		IconURL: fmt.Sprintf("https://files.coinmarketcap.com/static/img/coins/32x32/%s.png", t.ID),
	}

	fields := make([]*discordgo.MessageEmbedField, 0)

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:  "Coin Market Cap Rank",
		Value: fmt.Sprintf("#%s", t.Rank),
	})

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Price USD",
		Value:  fmt.Sprintf("$%s", t.PriceUSD),
		Inline: true,
	})

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Price BTC",
		Value:  fmt.Sprintf("%s BTC", t.PriceBTC),
		Inline: true,
	})

	parsedCap, err := strconv.ParseInt(t.LastUpdated, 10, 64)
	if err == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  "Market Cap",
			Value: fmt.Sprintf("$%s", humanize.Comma(parsedCap)),
		})
	}

	parsed1H, err := strconv.ParseFloat(t.Change1H, 64)
	if err == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Percent Change 1 hour",
			Value:  fmt.Sprintf("%.2f%%", parsed1H),
			Inline: true,
		})
	}

	parsed24H, err := strconv.ParseFloat(t.Change24H, 64)
	if err == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Percent Change 1 hour",
			Value:  fmt.Sprintf("%.2f%%", parsed24H),
			Inline: true,
		})
	}

	parsed7D, err := strconv.ParseFloat(t.Change7D, 64)
	if err == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Percent Change 1 hour",
			Value:  fmt.Sprintf("%.2f%%", parsed7D),
			Inline: true,
		})
	}

	embed.Fields = fields

	return &embed
}

func findTicker(t string) (tickerResponse, bool) {
	for _, ticker := range tickers {
		if strings.EqualFold(ticker.Name, t) || strings.EqualFold(ticker.Symbol, t) {
			return ticker, true
		}
	}

	var nullTicker tickerResponse
	return nullTicker, false
}

func isInSlice(s string, slice []string) bool {
	for _, str := range slice {
		if str == s {
			return true
		}
	}

	return false
}

func unixToTime(stamp int64) time.Time {
	return time.Unix(stamp, 0)
}
