package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"flag"

	"github.com/miekg/dns"
)

var (
	siteID            int
	reflectedDomain   string
	originalDomain    string
	dnsResolver       *net.Resolver
	serverPort        string
	force4via6        bool
	isTestingMode     bool  // Flag to skip initialization during tests
)

func init() {
	// Check for testing mode
	if flag.Lookup("test.v") != nil || os.Getenv("TESTING_MODE") == "true" {
		isTestingMode = true
		return
	}

	var err error
	
	siteIDStr, exists := os.LookupEnv("SITE_ID")
	if !exists {
		log.Fatalf("SITE_ID environment variable is required")
	}
	
	siteID, err = strconv.Atoi(siteIDStr)
	if err != nil {
		log.Fatalf("Invalid SITE_ID: %v", err)
	}
	if siteID < 0 || siteID > 65535 {
		log.Fatalf("SITE_ID must be between 0 and 65535")
	}

	reflectedDomain, exists = os.LookupEnv("REFLECTED_DOMAIN")
	if !exists {
		log.Fatalf("REFLECTED_DOMAIN environment variable is required")
	}

	originalDomain, exists = os.LookupEnv("ORIGINAL_DOMAIN")
	if !exists {
		log.Fatalf("ORIGINAL_DOMAIN environment variable is required")
	}

	// Ensure domains end with a dot for proper DNS comparison
	if !strings.HasSuffix(reflectedDomain, ".") {
		reflectedDomain = reflectedDomain + "."
	}
	if !strings.HasSuffix(originalDomain, ".") {
		originalDomain = originalDomain + "."
	}
	
	log.Printf("Domains configured: reflected_domain=%q, original_domain=%q", 
		reflectedDomain, originalDomain)
	
	// Check for FORCE_4VIA6 flag
	force4via6Str, exists := os.LookupEnv("FORCE_4VIA6")
	if exists {
		if strings.ToLower(force4via6Str) == "true" || force4via6Str == "1" {
			force4via6 = true
			log.Printf("FORCE_4VIA6 enabled: A queries will be answered with AAAA records")
		} else if strings.ToLower(force4via6Str) == "false" || force4via6Str == "0" {
			force4via6 = false
			log.Printf("FORCE_4VIA6 disabled: A queries will be answered with A records")
		} else {
			log.Printf("Invalid FORCE_4VIA6 value: %s - must be 'true', 'false', '1', or '0'. Defaulting to true", force4via6Str)
			force4via6 = true
		}
	} else {
		// Default to true for backward compatibility
		force4via6 = true
		log.Printf("FORCE_4VIA6 not specified, defaulting to true (A queries will be answered with AAAA records)")
	}
	
	// Check for custom server port
	if port, exists := os.LookupEnv("PORT"); exists && port != "" {
		// Validate the port is a number
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 {
			log.Fatalf("Invalid PORT value: %s - must be between 1 and 65535", port)
		}
		serverPort = port
		log.Printf("Using custom server port: %s", serverPort)
	} else {
		// Default to port 53 if not specified
		serverPort = "53"
		log.Printf("Using default DNS port: %s", serverPort)
	}

	// Set up the DNS resolver
	if customResolver, exists := os.LookupEnv("DNS_RESOLVER"); exists && customResolver != "" {
		// If a custom resolver was specified, use it
		// Format should be host:port (e.g., 8.8.8.8:53)
		log.Printf("Using custom DNS resolver: %s", customResolver)
		
		dnsResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "udp", customResolver)
			},
		}
	} else {
		// Otherwise use the system resolver
		log.Printf("Using system DNS resolver")
		dnsResolver = net.DefaultResolver
	}
}

// IPv4ToTailscale4via6 converts an IPv4 address to a Tailscale 4via6 IPv6 address
func IPv4ToTailscale4via6(ipv4 net.IP, siteID int) (net.IP, error) {
	if ipv4 == nil || ipv4.To4() == nil {
		return nil, fmt.Errorf("invalid IPv4 address")
	}

	// Tailscale 4via6 format: fd7a:115c:a1e0:b1a:0:XXXX:YYYY:YYYY
	// Where XXXX is the site ID and YYYY:YYYY is the IPv4 address in hex
	ipv4Bytes := ipv4.To4()
	ipv6 := make(net.IP, 16)

	// Set the fixed prefix fd7a:115c:a1e0:b1a
	ipv6[0] = 0xfd
	ipv6[1] = 0x7a
	ipv6[2] = 0x11
	ipv6[3] = 0x5c
	ipv6[4] = 0xa1
	ipv6[5] = 0xe0
	ipv6[6] = 0x0b
	ipv6[7] = 0x1a

	// Set the site ID (0:XXXX)
	ipv6[8] = 0x00
	ipv6[9] = 0x00
	ipv6[10] = byte(siteID >> 8)
	ipv6[11] = byte(siteID)

	// Set the IPv4 address (YYYY:YYYY)
	ipv6[12] = ipv4Bytes[0]
	ipv6[13] = ipv4Bytes[1]
	ipv6[14] = ipv4Bytes[2]
	ipv6[15] = ipv4Bytes[3]

	return ipv6, nil
}

// getDomainConversion converts a hostname from reflected domain to original domain
func getDomainConversion(hostname string, reflectedDomain string, originalDomain string) string {
	// Ensure hostname has trailing dot for proper handling
	if !strings.HasSuffix(hostname, ".") {
		hostname = hostname + "."
	}
	
	// Check if the hostname has the reflected domain suffix
	if strings.HasSuffix(hostname, reflectedDomain) {
		// Extract hostname without the reflected domain
		prefix := strings.TrimSuffix(hostname, reflectedDomain)
		
		// Handle case where prefix is empty (exact domain match)
		if prefix == "" {
			return originalDomain
		}
		
		// Handle case where prefix ends with a dot
		prefix = strings.TrimSuffix(prefix, ".")
		
		// Reconstruct the hostname with the original domain
		if prefix != "" {
			return prefix + "." + strings.TrimPrefix(originalDomain, ".")
		}
		return originalDomain
	}
	
	// If it doesn't have the suffix, return the original hostname
	return hostname
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	ctx := context.Background()

	for _, q := range r.Question {
		log.Printf("Received query for %s (type: %d)", q.Name, q.Qtype)
		
		// Debug suffix check - ensure proper comparison with trailing dots
		questionName := q.Name
		if !strings.HasSuffix(questionName, ".") {
			questionName = questionName + "."
		}
		
		isSuffix := strings.HasSuffix(questionName, reflectedDomain)
		log.Printf("Checking if %q has suffix %q: %v", questionName, reflectedDomain, isSuffix)

		// Check if the query is for our reflected domain
		if isSuffix {
			// Convert the domain using the new helper function
			originalName := getDomainConversion(questionName, reflectedDomain, originalDomain)
			
			log.Printf("Converting hostname: %s -> %s", questionName, originalName)
			
			// Handle AAAA queries
			if q.Qtype == dns.TypeAAAA {
				log.Printf("AAAA query for %s, looking up AAAA records for %s", q.Name, originalName)
				
				// First try to get native AAAA records from original domain
				ips, err := dnsResolver.LookupIP(ctx, "ip6", strings.TrimSuffix(originalName, "."))
				hasNativeAAAA := false
				
				if err == nil && len(ips) > 0 {
					// Process any IPv6 addresses we found directly
					for _, ip := range ips {
						if ip.To4() == nil { // This is a proper IPv6 address
							log.Printf("Found native IPv6 address: %s", ip)
							aaaa := &dns.AAAA{
								Hdr: dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeAAAA,
									Class:  dns.ClassINET,
									Ttl:    300,
								},
								AAAA: ip,
							}
							m.Answer = append(m.Answer, aaaa)
							hasNativeAAAA = true
						}
					}
				}
				
				// If we found native AAAA records, we're done with this question
				if hasNativeAAAA {
						log.Printf("Returning native IPv6 addresses for %s", q.Name)
						continue
					}
				
				// Otherwise, fall back to A record lookup and conversion
				log.Printf("No native AAAA records found, falling back to A record lookup and conversion")
				ips, err = dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(originalName, "."))
				if err != nil {
					log.Printf("Error looking up A record: %v", err)
					continue
				}

				// Convert A records to AAAA and add to response
				addConvertedARecords(m, q.Name, ips)
			} else if q.Qtype == dns.TypeA {
				// Handle A queries
				if force4via6 {
					// With force4via6 enabled, return AAAA records for A queries
					log.Printf("A query with force4via6=true: looking up A records for %s and converting to AAAA", originalName)
					ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(originalName, "."))
			if err != nil {
				log.Printf("Error looking up A record: %v", err)
				continue
			}

					// Convert A records to AAAA and add to response
					addConvertedARecords(m, q.Name, ips)
				} else {
					// Without force4via6, return normal A records
					log.Printf("A query with force4via6=false: looking up A records for %s", originalName)
					ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(originalName, "."))
					if err != nil {
						log.Printf("Error looking up A record: %v", err)
						continue
					}

					// Add A records to response
					addARecords(m, q.Name, ips)
				}
			}
		} else {
			// Forward other queries to the system resolver
			log.Printf("Forwarding query for %s (type %d)", q.Name, q.Qtype)

			switch q.Qtype {
			case dns.TypeA:
				ips, err := dnsResolver.LookupIP(ctx, "ip4", strings.TrimSuffix(q.Name, "."))
				if err == nil {
					addARecords(m, q.Name, ips)
				}
			case dns.TypeAAAA:
				ips, err := dnsResolver.LookupIP(ctx, "ip6", strings.TrimSuffix(q.Name, "."))
				if err == nil {
					for _, ip := range ips {
						if ip.To4() == nil {
							aaaa := &dns.AAAA{
								Hdr: dns.RR_Header{
									Name:   q.Name,
									Rrtype: dns.TypeAAAA,
									Class:  dns.ClassINET,
									Ttl:    300,
								},
								AAAA: ip,
							}
							m.Answer = append(m.Answer, aaaa)
						}
					}
				}
			}
		}
	}

	w.WriteMsg(m)
}

// addARecords adds A records to the DNS message
func addARecords(m *dns.Msg, name string, ips []net.IP) {
	for _, ip := range ips {
		if ip.To4() != nil {
			log.Printf("Adding A record: %s for %s", ip, name)
			a := &dns.A{
				Hdr: dns.RR_Header{
					Name:   name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				A: ip.To4(),
			}
			m.Answer = append(m.Answer, a)
		}
	}
}

// addConvertedARecords converts IPv4 addresses to Tailscale 4via6 format and adds them as AAAA records
func addConvertedARecords(m *dns.Msg, name string, ips []net.IP) {
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv6, err := IPv4ToTailscale4via6(ip, siteID)
			if err != nil {
				log.Printf("Error converting to IPv6: %v", err)
				continue
			}

			log.Printf("Converting %s to %s for %s", ip, ipv6, name)
			
			aaaa := &dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   name,
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				AAAA: ipv6,
			}
			m.Answer = append(m.Answer, aaaa)
		}
	}
}

func main() {
	log.Printf("Starting tsdnsreflector with SITE_ID=%d, REFLECTED_DOMAIN=%s, ORIGINAL_DOMAIN=%s, PORT=%s, FORCE_4VIA6=%v", 
		siteID, reflectedDomain, originalDomain, serverPort, force4via6)

	// Create a new DNS server
	dns.HandleFunc(".", handleDNSRequest)

	// Start the DNS server
	go func() {
		server := &dns.Server{Addr: ":" + serverPort, Net: "udp"}
		log.Printf("Starting DNS server on %s/udp", server.Addr)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start UDP server: %v", err)
		}
	}()

	go func() {
		server := &dns.Server{Addr: ":" + serverPort, Net: "tcp"}
		log.Printf("Starting DNS server on %s/tcp", server.Addr)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
		}
	}()

	log.Printf("Server ready and listening on port %s", serverPort)

	// Keep the server running
	select {}
} 