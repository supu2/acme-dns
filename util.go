package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	log "github.com/Sirupsen/logrus"
	"github.com/iris-contrib/middleware/cors"
	"github.com/kataras/iris"
	"github.com/miekg/dns"
	"github.com/satori/go.uuid"
	"math/big"
	"os"
	"regexp"
	"strings"
)

func readConfig(fname string) (DNSConfig, error) {
	var conf DNSConfig
	if _, err := toml.DecodeFile(fname, &conf); err != nil {
		return DNSConfig{}, errors.New("Malformed configuration file")
	}
	return conf, nil
}

func sanitizeString(s string) string {
	// URL safe base64 alphabet without padding as defined in ACME
	re, _ := regexp.Compile("[^A-Za-z\\-\\_0-9]+")
	return re.ReplaceAllString(s, "")
}

func generatePassword(length int) string {
	ret := make([]byte, length)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz1234567890-_"
	alphalen := big.NewInt(int64(len(alphabet)))
	for i := 0; i < length; i++ {
		c, _ := rand.Int(rand.Reader, alphalen)
		r := int(c.Int64())
		ret[i] = alphabet[r]
	}
	return string(ret)
}

func sanitizeDomainQuestion(d string) string {
	dom := strings.ToLower(d)
	firstDot := strings.Index(d, ".")
	if firstDot > 0 {
		dom = dom[0:firstDot]
	}
	fmt.Printf("%s\n", dom)
	return dom
}

func newACMETxt() (ACMETxt, error) {
	var a = ACMETxt{}
	password := generatePassword(40)
	a.Username = uuid.NewV4()
	a.Password = password
	a.Subdomain = uuid.NewV4().String()
	return a, nil
}

func setupLogging(format string, level string) {
	if DNSConf.Logconfig.Format == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}
	switch level {
	default:
		log.SetLevel(log.WarnLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	}
	// TODO: file logging
}

func startDNS(listen string) *dns.Server {
	// DNS server part
	dns.HandleFunc(".", handleRequest)
	server := &dns.Server{Addr: listen, Net: "udp"}
	go func() {
		err := server.ListenAndServe()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	}()
	return server
}

func startHTTPAPI() {
	api := iris.New()
	api.Config.DisableBanner = true
	crs := cors.New(cors.Options{
		AllowedOrigins:     DNSConf.API.CorsOrigins,
		AllowedMethods:     []string{"GET", "POST"},
		OptionsPassthrough: false,
		Debug:              DNSConf.General.Debug,
	})
	api.Use(crs)
	var ForceAuth = authMiddleware{}
	api.Get("/register", webRegisterGet)
	api.Post("/register", webRegisterPost)
	api.Post("/update", ForceAuth.Serve, webUpdatePost)
	switch DNSConf.API.TLS {
	case "letsencrypt":
		listener, err := iris.LETSENCRYPTPROD(DNSConf.API.Domain)
		err = api.Serve(listener)
		if err != nil {
			log.Errorf("Error in HTTP server [%v]", err)
		}
	case "cert":
		host := DNSConf.API.Domain + ":" + DNSConf.API.Port
		api.ListenTLS(host, DNSConf.API.TLSCertFullchain, DNSConf.API.TLSCertPrivkey)
	default:
		host := DNSConf.API.Domain + ":" + DNSConf.API.Port
		api.Listen(host)
	}
}
