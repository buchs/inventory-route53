package main

// This program scans through all AWS Route53 domain names in a single
// account, looking for ones which are no longer valid. A DNS name can
// become invalid when: the EC2 or ELB the names are pointing to is
// terminated. It is especially troublesome when an EC2's domain name,
// is used for an alias and the public IP address is now in use in someone
// else's AWS account. 

// To use this, first run:
// 1. instance-inventory-brief-all.py -- which generates a list of all
//    current EC2s public domain names and public IP 
//    addresses into the file public.csv. This looks at EC2s in all
//    AWS accounts. 
// 2. elb-inventory-brief.py -- which generates a list of the public
//    domain names for all running elastic load balancers in all AWS 
//    accounts. Creates the file elbs.txt
//
// Then you can run this program, which will read those two files as
// input data. The output is written to two files: 
// rt53-recog-targets.csv and rt53-unkno-targets.csv. The first gives
// all the (possibly) valid route53 domain names and the second gives
// the ones found to be invalid. I say "possibly" valid, because all
// that is implied is that they passed the tests for validity in this
// program, but may still be invalid for other tests.

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"golang.org/x/net/http2"
)

var knownDns []string
var knownIps []string
// This contains domains which are good choices for making aliases
var ripeDomains [4]*regexp.Regexp 

// HTTPClientSettings is used to perform some optimizations for
// the http client to work well with AWS.
type HTTPClientSettings struct {
	Connect          time.Duration
	ConnKeepAlive    time.Duration
	ExpectContinue   time.Duration
	IdleConn         time.Duration
	MaxAllIdleConns  int
	MaxHostIdleConns int
	ResponseHeader   time.Duration
	TLSHandshake     time.Duration
}

func setupConstants() {
	ripeDomains[0] = regexp.MustCompile("^.*\\.amazonaws\\.com$")
	ripeDomains[1] = regexp.MustCompile("^.*\\.amazonses\\.com$")
	ripeDomains[2] = regexp.MustCompile("^.*\\.cloudfront\\.net$")
	ripeDomains[3] = regexp.MustCompile("^.*\\.example\\.com$")
}

// NewHTTPClientWithSettings Set up new HTTP client - with customizations
func NewHTTPClientWithSettings(httpSettings HTTPClientSettings) *http.Client {

	tr := &http.Transport{
		ResponseHeaderTimeout: httpSettings.ResponseHeader,
		Proxy:                 http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			KeepAlive: httpSettings.ConnKeepAlive,
			DualStack: true,
			Timeout:   httpSettings.Connect,
		}).DialContext,
		MaxIdleConns:          httpSettings.MaxAllIdleConns,
		IdleConnTimeout:       httpSettings.IdleConn,
		TLSHandshakeTimeout:   httpSettings.TLSHandshake,
		MaxIdleConnsPerHost:   httpSettings.MaxHostIdleConns,
		ExpectContinueTimeout: httpSettings.ExpectContinue,
	}

	// So client makes HTTP/2 requests
	http2.ConfigureTransport(tr)

	return &http.Client{
		Transport: tr,
	}
}

func loadData() {
	dnRegexp := regexp.MustCompile(".*\\..*")
	ipRegexp := regexp.MustCompile("([0-9]+\\.){3}[0-9]+")
	file, err := os.Open("public.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		t := scanner.Text()
		if t != "" {
			if ipRegexp.MatchString(t) {
				knownIps = append(knownIps, t)
			} else if dnRegexp.MatchString(t) {
				knownDns = append(knownDns, t)
			}
		}
	}
	err = scanner.Err()
	if err != nil {
		fmt.Println("scanner error: ", err)
	}

	file2, err := os.Open("elbs.txt")
	if err != nil {
		panic(err)
	}
	defer file2.Close()
	scanner2 := bufio.NewScanner(file2)
	for scanner2.Scan() {
		t := scanner2.Text()
		if t != "" {
			knownDns = append(knownDns, t)
		}
	}
	err = scanner2.Err()
	if err != nil {
		fmt.Println("scanner 2 error: ", err)
	}

}

func ipKnown(ip string) bool {

	for _, v := range knownIps {
		if ip == v {
			return true
		}
	}
	return false
}

func dnKnown(dn string) bool {

	for _, v := range knownDns {
		if dn == v {
			return true
		}
	}
	return false
}

func ripeDomainCk(target string) string {
	for _, checkDomain := range ripeDomains {
		if (*checkDomain).MatchString(target) {
			return ", Can be alias"
		}
	}
	return ""
}

func main() {

	setupConstants()
	loadData()

	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String("us-west-2"),
		Credentials: credentials.NewSharedCredentials("", "dns-account"),
		HTTPClient: NewHTTPClientWithSettings(HTTPClientSettings{
			Connect:          5 * time.Second,
			ExpectContinue:   1 * time.Second,
			IdleConn:         90 * time.Second,
			ConnKeepAlive:    30 * time.Second,
			MaxAllIdleConns:  100,
			MaxHostIdleConns: 10,
			ResponseHeader:   5 * time.Second,
			TLSHandshake:     5 * time.Second,
		})}))

	rt53 := route53.New(sess)
	r53hostedZone := "<put yours here>"
	r53input := route53.ListResourceRecordSetsInput{HostedZoneId: &r53hostedZone}
	r53output, err := rt53.ListResourceRecordSets(&r53input)
	counter := 0

	var name, target, suitableForAlias string

	fpReconTarget, err := os.Create("rt53-recog-targets.csv")
	if err != nil {
		panic(err)
	}
	defer fpReconTarget.Close()
	fpUnTarget, err := os.Create("rt53-unkno-targets.csv")
	if err != nil {
		panic(err)
	}
	defer fpUnTarget.Close()

	for err == nil {
		records := r53output.ResourceRecordSets
		for _, r := range records {
			rr := *r
			rtype := *rr.Type
			if rtype == "A" || rtype == "CNAME" {
				fullname := *rr.Name
				if strings.HasSuffix(fullname, ".") {
					name = strings.TrimRight(fullname, ".")
				} else {
					name = fullname
				}
				if rtype == "CNAME" {
					if r.AliasTarget != nil {
						target = strings.TrimRight(*(*rr.AliasTarget).DNSName, ".")
						if dnKnown(target) {
							fmt.Fprintf(fpReconTarget, "%d, %s, %s, %s, alias\n",
								counter, name, rtype, target)
						} else {
							fmt.Fprintf(fpUnTarget, "%d, %s, %s, %s, alias\n",
								counter, name, rtype, target)
						}
					} else {
						// Expect array of struct with Value field
						if len(rr.ResourceRecords) == 0 {
							// This should be considered an error:
							fmt.Printf("funnie:%s", rr.ResourceRecords)
							continue
						} else {
							for _, rec := range rr.ResourceRecords {
								target = strings.Trim(*rec.Value, "\n\r\t ")
								suitableForAlias = ripeDomainCk(target)
								if dnKnown(target) {
									fmt.Fprintf(fpReconTarget, "%d, %s, %s, %s%s\n",
										counter, name, rtype, target, suitableForAlias)
								} else {
									fmt.Fprintf(fpUnTarget, "%d, %s, %s, %s%s\n",
										counter, name, rtype, target, suitableForAlias)
								}
							}
						}
					}
				} else if rtype == "A" {
					if r.AliasTarget != nil {
						target = strings.TrimRight(*(*rr.AliasTarget).DNSName, ".")
						if dnKnown(target) {
							fmt.Fprintf(fpReconTarget, "%d, %s, %s, %s, alias\n",
								counter, name, rtype, target)
						} else {
							fmt.Fprintf(fpUnTarget, "%d, %s, %s, %s, alias\n",
								counter, name, rtype, target)
						}
					} else {
						vo := reflect.ValueOf(rr.ResourceRecords).Kind()
						switch vo {
						case reflect.Slice:
							for _, rec := range rr.ResourceRecords {
								target = *(*rec).Value
								suitableForAlias = ripeDomainCk(target)
								if ipKnown(target) {
									fmt.Fprintf(fpReconTarget, "%d, %s, %s, %s%s\n",
										counter, name, rtype, target, suitableForAlias)
								} else {
									fmt.Fprintf(fpUnTarget, "%d, %s, %s, %s%s\n",
										counter, name, rtype, target, suitableForAlias)
								}
							}
						default:
							fmt.Printf("%d, %s, %s, ", counter, name, rtype)
							fmt.Print("<< Kind:  ", vo, "DNS Name: %+v\n", rr.ResourceRecords)
						}
					}
				}
				counter++
			}
		}
		if *r53output.IsTruncated {
			r53input.StartRecordName = r53output.NextRecordName
			r53input.StartRecordType = r53output.NextRecordType
			r53input.StartRecordIdentifier = r53output.NextRecordIdentifier
		} else {
			break
		}
		r53output, err = rt53.ListResourceRecordSets(&r53input)
	}
}
