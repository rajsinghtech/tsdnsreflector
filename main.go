package main

import (
	"flag"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

type Config struct {
	SiteID          int
	ReflectedDomain string
	OriginalDomain  string
	DNSResolver     string
	Force4via6      bool
	ListenAddr      string
}

func main() {
	config := parseConfig()

	log.Printf("Starting tsdnsreflector with config: %+v", config)

	dns.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		handleDNSRequest(w, r, config)
	})

	server := &dns.Server{
		Addr: config.ListenAddr,
		Net:  "udp",
	}

	log.Printf("DNS reflector listening on %s", config.ListenAddr)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start server: %s", err.Error())
	}
}

func parseConfig() Config {
	var config Config

	// Command line flags
	flag.IntVar(&config.SiteID, "siteid", 0, "Site ID for 4via6 conversion")
	flag.StringVar(&config.ReflectedDomain, "reflected-domain", "", "Reflected domain (e.g. cluster1.local)")
	flag.StringVar(&config.OriginalDomain, "original-domain", "", "Original domain (e.g. cluster.local)")
	flag.StringVar(&config.DNSResolver, "dns-resolver", "", "DNS resolver to use (IPv6 address, empty for host resolver)")
	flag.BoolVar(&config.Force4via6, "force-4via6", false, "Force 4via6 conversion for A records")
	flag.StringVar(&config.ListenAddr, "listen", ":53", "Address to listen on (e.g. :53)")
	flag.Parse()

	// Environment variables override flags
	if siteIDStr := os.Getenv("SITEID"); siteIDStr != "" {
		if siteID, err := strconv.Atoi(siteIDStr); err == nil {
			config.SiteID = siteID
		}
	}
	if reflectedDomain := os.Getenv("REFLECTED_DOMAIN"); reflectedDomain != "" {
		config.ReflectedDomain = reflectedDomain
	}
	if originalDomain := os.Getenv("ORIGINAL_DOMAIN"); originalDomain != "" {
		config.OriginalDomain = originalDomain
	}
	if dnsResolver := os.Getenv("DNS_RESOLVER"); dnsResolver != "" {
		config.DNSResolver = dnsResolver
	}
	if force4via6Str := os.Getenv("FORCE_4VIA6"); force4via6Str != "" {
		if force4via6, err := strconv.ParseBool(force4via6Str); err == nil {
			config.Force4via6 = force4via6
		}
	}
	if listenAddr := os.Getenv("LISTEN_ADDR"); listenAddr != "" {
		config.ListenAddr = listenAddr
	}

	// Validate required fields
	if config.ReflectedDomain == "" || config.OriginalDomain == "" {
		log.Fatal("Reflected domain and original domain must be specified")
	}

	// Ensure domains end with a dot for comparison
	if !strings.HasSuffix(config.ReflectedDomain, ".") {
		config.ReflectedDomain = config.ReflectedDomain + "."
	}
	if !strings.HasSuffix(config.OriginalDomain, ".") {
		config.OriginalDomain = config.OriginalDomain + "."
	}

	return config
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg, config Config) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		log.Printf("Query: %s %s", dns.TypeToString[q.Qtype], q.Name)

		// Only process queries for the reflected domain
		if !strings.HasSuffix(q.Name, config.ReflectedDomain) {
			log.Printf("Not a reflected domain query, passing through")
			passthrough(w, r, config)
			return
		}

		// Convert reflected domain to original domain
		originalName := strings.Replace(q.Name, config.ReflectedDomain, config.OriginalDomain, 1)
		log.Printf("Converted to original domain: %s", originalName)

		switch q.Qtype {
		case dns.TypeA:
			handleAQuery(w, r, m, q, originalName, config)
			return
		case dns.TypeAAAA:
			// Pass through AAAA queries directly
			handleAAAAQuery(w, r, m, q, originalName, config)
			return
		default:
			// For other query types, just pass through
			passthrough(w, r, config)
			return
		}
	}

	// Empty response for no questions
	w.WriteMsg(m)
}

func handleAQuery(w dns.ResponseWriter, r *dns.Msg, m *dns.Msg, q dns.Question, originalName string, config Config) {
	// Resolve A record for the original domain
	var ipv4Addrs []net.IP
	var err error

	if config.DNSResolver == "none" || config.DNSResolver == "" {
		// Use host resolver
		ipv4Addrs, err = lookupHostA(originalName)
	} else {
		// Use specified resolver
		ipv4Addrs, err = lookupHostAWithResolver(originalName, config.DNSResolver)
	}

	if err != nil {
		log.Printf("Error resolving A record for %s: %v", originalName, err)
		m.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(m)
		return
	}

	log.Printf("Resolved A records for %s: %v", originalName, ipv4Addrs)

	if config.Force4via6 {
		// Convert A records to AAAA using 4via6
		for _, ip := range ipv4Addrs {
			if ipv4 := ip.To4(); ipv4 != nil {
				aaaa := convertTo4via6(ipv4, config.SiteID)
				m.Answer = append(m.Answer, &dns.AAAA{
					Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
					AAAA: aaaa,
				})
				log.Printf("Converted %v to AAAA: %v", ipv4, aaaa)
			}
		}
	} else {
		// Return original A records
		for _, ip := range ipv4Addrs {
			if ipv4 := ip.To4(); ipv4 != nil {
				m.Answer = append(m.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   ipv4,
				})
			}
		}
	}

	w.WriteMsg(m)
}

func handleAAAAQuery(w dns.ResponseWriter, r *dns.Msg, m *dns.Msg, q dns.Question, originalName string, config Config) {
	// Just resolve AAAA for the original domain
	var ipv6Addrs []net.IP
	var err error

	if config.DNSResolver == "none" || config.DNSResolver == "" {
		// Use host resolver
		ipv6Addrs, err = lookupHostAAAA(originalName)
	} else {
		// Use specified resolver
		ipv6Addrs, err = lookupHostAAAAWithResolver(originalName, config.DNSResolver)
	}

	if err != nil {
		log.Printf("Error resolving AAAA record for %s: %v", originalName, err)
		m.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(m)
		return
	}

	log.Printf("Resolved AAAA records for %s: %v", originalName, ipv6Addrs)

	for _, ip := range ipv6Addrs {
		if ipv6 := ip.To16(); ipv6 != nil && !ip.To4().Equal(ipv6) { // Ensure it's IPv6, not IPv4
			m.Answer = append(m.Answer, &dns.AAAA{
				Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
				AAAA: ipv6,
			})
		}
	}

	w.WriteMsg(m)
}

func passthrough(w dns.ResponseWriter, r *dns.Msg, config Config) {
	if config.DNSResolver == "none" || config.DNSResolver == "" {
		// If no resolver specified, use host resolver
		// Create client with default resolver
		c := new(dns.Client)
		resp, _, err := c.Exchange(r, "127.0.0.1:53")
		if err != nil {
			log.Printf("Error in passthrough resolution: %v", err)
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(m)
			return
		}
		w.WriteMsg(resp)
	} else {
		// Use the specified resolver
		c := new(dns.Client)
		resp, _, err := c.Exchange(r, net.JoinHostPort(config.DNSResolver, "53"))
		if err != nil {
			log.Printf("Error in passthrough resolution with resolver %s: %v", config.DNSResolver, err)
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(m)
			return
		}
		w.WriteMsg(resp)
	}
}

func lookupHostA(hostname string) ([]net.IP, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}

	var ipv4s []net.IP
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			ipv4s = append(ipv4s, ipv4)
		}
	}
	return ipv4s, nil
}

func lookupHostAAAA(hostname string) ([]net.IP, error) {
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}

	var ipv6s []net.IP
	for _, ip := range ips {
		if ip.To4() == nil && ip.To16() != nil {
			ipv6s = append(ipv6s, ip)
		}
	}
	return ipv6s, nil
}

func lookupHostAWithResolver(hostname, resolver string) ([]net.IP, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(hostname), dns.TypeA)
	m.RecursionDesired = true

	c := new(dns.Client)
	r, _, err := c.Exchange(m, net.JoinHostPort(resolver, "53"))
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			ips = append(ips, a.A)
		}
	}
	return ips, nil
}

func lookupHostAAAAWithResolver(hostname, resolver string) ([]net.IP, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(hostname), dns.TypeAAAA)
	m.RecursionDesired = true

	c := new(dns.Client)
	r, _, err := c.Exchange(m, net.JoinHostPort(resolver, "53"))
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	for _, ans := range r.Answer {
		if aaaa, ok := ans.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA)
		}
	}
	return ips, nil
}

// convertTo4via6 converts an IPv4 address to a Tailscale 4via6 IPv6 address
// Format: fd7a:115c:a1e0:b1a:0:XXXX:YYYY:YYYY
// where XXXX is the site ID and YYYY:YYYY is the IPv4 in hex
func convertTo4via6(ipv4 net.IP, siteID int) net.IP {
	// Fixed prefix for Tailscale 4via6
	prefix := []byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 0x0b, 0x1a, 0x00, 0x00}

	// Site ID (two bytes)
	siteIDBytes := []byte{byte(siteID >> 8), byte(siteID)}

	// IPv4 address
	ipv4Bytes := ipv4.To4()

	// Create IPv6 address
	ipv6 := append(prefix, siteIDBytes...)
	ipv6 = append(ipv6, ipv4Bytes...)

	return net.IP(ipv6)
}
