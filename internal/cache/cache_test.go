package cache

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestCacheMemoryCalculation(t *testing.T) {
	cache := NewZoneCache(10, time.Minute)
	defer cache.Stop()

	t.Run("empty_dns_message", func(t *testing.T) {
		key := "test.example.com:A"
		msg := &dns.Msg{}
		
		size := cache.calculateEntrySize(key, msg)
		
		// Should include key size + minimal DNS message + entry struct + map overhead
		expectedMinSize := int64(len(key)) + 50 // Minimum reasonable size
		if size < expectedMinSize {
			t.Errorf("Memory calculation too small: got %d, expected at least %d", size, expectedMinSize)
		}
		
		t.Logf("Empty DNS message size: %d bytes", size)
	})

	t.Run("simple_a_record", func(t *testing.T) {
		key := "test.example.com:A"
		msg := createSimpleARecord()
		
		size := cache.calculateEntrySize(key, msg)
		
		// Should be significantly larger than empty message
		expectedMinSize := int64(200) // Conservative estimate
		if size < expectedMinSize {
			t.Errorf("A record memory calculation too small: got %d, expected at least %d", size, expectedMinSize)
		}
		
		t.Logf("Simple A record size: %d bytes", size)
	})

	t.Run("complex_dns_message", func(t *testing.T) {
		key := "complex.example.com:A"
		msg := createComplexDNSMessage()
		
		size := cache.calculateEntrySize(key, msg)
		
		// Complex message with multiple records should be much larger
		expectedMinSize := int64(500)
		if size < expectedMinSize {
			t.Errorf("Complex DNS message memory calculation too small: got %d, expected at least %d", size, expectedMinSize)
		}
		
		t.Logf("Complex DNS message size: %d bytes", size)
	})

	t.Run("txt_record_with_long_strings", func(t *testing.T) {
		key := "txt.example.com:TXT"
		msg := createTXTRecord()
		
		size := cache.calculateEntrySize(key, msg)
		
		// TXT records with long strings should have significant memory usage
		expectedMinSize := int64(300)
		if size < expectedMinSize {
			t.Errorf("TXT record memory calculation too small: got %d, expected at least %d", size, expectedMinSize)
		}
		
		t.Logf("TXT record size: %d bytes", size)
	})
}

func TestCacheMemoryAccuracy(t *testing.T) {
	cache := NewZoneCache(10, time.Minute)
	defer cache.Stop()

	t.Run("memory_usage_tracking", func(t *testing.T) {
		// Start with empty cache
		if cache.MemoryUsage() != 0 {
			t.Errorf("Expected empty cache memory usage to be 0, got %d", cache.MemoryUsage())
		}

		// Add a record
		key := "test1.example.com:A"
		msg := createSimpleARecord()
		cache.Set(key, msg)

		usage1 := cache.MemoryUsage()
		if usage1 <= 0 {
			t.Errorf("Expected positive memory usage after adding record, got %d", usage1)
		}

		// Add another record
		key2 := "test2.example.com:A"
		msg2 := createComplexDNSMessage()
		cache.Set(key2, msg2)

		usage2 := cache.MemoryUsage()
		if usage2 <= usage1 {
			t.Errorf("Expected memory usage to increase after adding second record: %d -> %d", usage1, usage2)
		}

		// Clear cache
		cache.Clear()
		if cache.MemoryUsage() != 0 {
			t.Errorf("Expected memory usage to be 0 after clear, got %d", cache.MemoryUsage())
		}

		t.Logf("Memory usage progression: 0 -> %d -> %d -> 0", usage1, usage2)
	})

	t.Run("memory_calculation_consistency", func(t *testing.T) {
		key := "consistency.example.com:A"
		msg := createSimpleARecord()
		
		// Calculate size before adding
		expectedSize := cache.calculateEntrySize(key, msg)
		
		// Add to cache
		cache.Set(key, msg)
		actualUsage := cache.MemoryUsage()
		
		if actualUsage != expectedSize {
			t.Errorf("Memory usage inconsistency: calculated %d, actual %d", expectedSize, actualUsage)
		}
		
		t.Logf("Consistent memory calculation: %d bytes", expectedSize)
	})
}

func TestDNSMessageSizeCalculation(t *testing.T) {
	cache := NewZoneCache(1, time.Minute)
	defer cache.Stop()

	t.Run("dns_message_components", func(t *testing.T) {
		msg := &dns.Msg{}
		
		// Empty message
		emptySize := cache.calculateDNSMsgSize(msg)
		
		// Add question
		msg.Question = []dns.Question{
			{Name: "test.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
		}
		questionSize := cache.calculateDNSMsgSize(msg)
		
		if questionSize <= emptySize {
			t.Errorf("Adding question should increase size: %d -> %d", emptySize, questionSize)
		}
		
		// Add answer
		rr := &dns.A{
			Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   []byte{192, 168, 1, 1},
		}
		msg.Answer = []dns.RR{rr}
		answerSize := cache.calculateDNSMsgSize(msg)
		
		if answerSize <= questionSize {
			t.Errorf("Adding answer should increase size: %d -> %d", questionSize, answerSize)
		}
		
		t.Logf("DNS size progression: empty=%d, +question=%d, +answer=%d", emptySize, questionSize, answerSize)
	})
}

func TestResourceRecordSizeCalculation(t *testing.T) {
	cache := NewZoneCache(1, time.Minute)
	defer cache.Stop()

	testCases := []struct {
		name     string
		rr       dns.RR
		minSize  int64
	}{
		{
			name: "A_record",
			rr: &dns.A{
				Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET},
				A:   []byte{192, 168, 1, 1},
			},
			minSize: 50,
		},
		{
			name: "AAAA_record",
			rr: &dns.AAAA{
				Hdr:  dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET},
				AAAA: []byte{0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			},
			minSize: 60,
		},
		{
			name: "CNAME_record",
			rr: &dns.CNAME{
				Hdr:    dns.RR_Header{Name: "alias.example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET},
				Target: "target.example.com.",
			},
			minSize: 80,
		},
		{
			name: "TXT_record",
			rr: &dns.TXT{
				Hdr: dns.RR_Header{Name: "txt.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET},
				Txt: []string{"this is a test TXT record", "with multiple strings", "for memory calculation testing"},
			},
			minSize: 120,
		},
		{
			name: "MX_record",
			rr: &dns.MX{
				Hdr:        dns.RR_Header{Name: "mail.example.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET},
				Preference: 10,
				Mx:         "mailserver.example.com.",
			},
			minSize: 80,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			size := cache.calculateRRSize(tc.rr)
			
			if size < tc.minSize {
				t.Errorf("%s memory calculation too small: got %d, expected at least %d", tc.name, size, tc.minSize)
			}
			
			t.Logf("%s size: %d bytes", tc.name, size)
		})
	}
}

// Helper functions to create test DNS messages

func createSimpleARecord() *dns.Msg {
	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn("test.example.com"), dns.TypeA)
	
	rr := &dns.A{
		Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   []byte{192, 168, 1, 1},
	}
	msg.Answer = []dns.RR{rr}
	
	return msg
}

func createComplexDNSMessage() *dns.Msg {
	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn("complex.example.com"), dns.TypeA)
	
	// Multiple answer records
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "complex.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   []byte{192, 168, 1, 1},
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "complex.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   []byte{192, 168, 1, 2},
		},
	}
	
	// Authority section
	msg.Ns = []dns.RR{
		&dns.NS{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 86400},
			Ns:  "ns1.example.com.",
		},
		&dns.NS{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 86400},
			Ns:  "ns2.example.com.",
		},
	}
	
	// Additional section
	msg.Extra = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "ns1.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 86400},
			A:   []byte{203, 0, 113, 1},
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "ns2.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 86400},
			A:   []byte{203, 0, 113, 2},
		},
	}
	
	return msg
}

func createTXTRecord() *dns.Msg {
	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn("txt.example.com"), dns.TypeTXT)
	
	rr := &dns.TXT{
		Hdr: dns.RR_Header{Name: "txt.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{
			"v=spf1 include:_spf.google.com ~all",
			"google-site-verification=abcdef123456789",
			"this is a very long TXT record for testing memory calculation accuracy with multiple text segments",
		},
	}
	msg.Answer = []dns.RR{rr}
	
	return msg
}

func TestCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		qtype    uint16
		clientIP []byte
		expected string
	}{
		{
			name:     "A record without client IP",
			domain:   "example.com.",
			qtype:    dns.TypeA,
			clientIP: nil,
			expected: "example.com.:A",
		},
		{
			name:     "AAAA record without client IP", 
			domain:   "test.local.",
			qtype:    dns.TypeAAAA,
			clientIP: nil,
			expected: "test.local.:AAAA",
		},
		{
			name:     "A record with client IP",
			domain:   "example.com.",
			qtype:    dns.TypeA,
			clientIP: []byte{192, 168, 1, 1},
			expected: "example.com.:A:192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CacheKey(tt.domain, tt.qtype, tt.clientIP)
			if result != tt.expected {
				t.Errorf("CacheKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestZoneCacheBasicOperations(t *testing.T) {
	cache := NewZoneCache(10, 5*time.Minute)
	defer cache.Stop()

	// Create test DNS message
	msg := createSimpleARecord()
	key := "test.com.:A"

	// Test cache miss
	result, found := cache.Get(key)
	if found {
		t.Errorf("Expected cache miss, but got hit")
	}
	if result != nil {
		t.Errorf("Expected nil result on cache miss")
	}

	// Test cache set
	cache.Set(key, msg)

	// Test cache hit
	result, found = cache.Get(key)
	if !found {
		t.Errorf("Expected cache hit, but got miss")
	}
	if result == nil {
		t.Errorf("Expected non-nil result on cache hit")
		return
	}

	// Verify response content
	if result.Id != msg.Id {
		t.Errorf("Cached response ID mismatch: got %d, want %d", result.Id, msg.Id)
	}

	// Test cache size
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}
}

func TestZoneCacheExpiration(t *testing.T) {
	cache := NewZoneCache(10, 100*time.Millisecond)
	defer cache.Stop()

	msg := createSimpleARecord()
	key := "test.com.:A"
	cache.Set(key, msg)

	// Verify cache hit before expiration
	result, found := cache.Get(key)
	if !found {
		t.Errorf("Expected cache hit before expiration")
	}
	if result == nil {
		t.Errorf("Expected non-nil result before expiration")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Verify cache miss after expiration
	result, found = cache.Get(key)
	if found {
		t.Errorf("Expected cache miss after expiration")
	}
	if result != nil {
		t.Errorf("Expected nil result after expiration")
	}
}

func TestZoneCacheEviction(t *testing.T) {
	cache := NewZoneCacheWithName(2, 5*time.Minute, "test-zone")
	defer cache.Stop()

	msg1 := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 1, Response: true}}
	msg2 := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 2, Response: true}}
	msg3 := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 3, Response: true}}

	// Fill cache to capacity
	cache.Set("key1", msg1)
	cache.Set("key2", msg2)

	if cache.Size() != 2 {
		t.Errorf("Expected cache size 2, got %d", cache.Size())
	}

	// Add third item - should trigger eviction
	cache.Set("key3", msg3)

	if cache.Size() != 2 {
		t.Errorf("Expected cache size to remain 2 after eviction, got %d", cache.Size())
	}

	// Verify oldest entry was evicted
	_, found := cache.Get("key1")
	if found {
		t.Errorf("Expected oldest entry to be evicted")
	}

	// Verify newer entries still exist
	_, found = cache.Get("key2")
	if !found {
		t.Errorf("Expected key2 to still exist")
	}

	_, found = cache.Get("key3")
	if !found {
		t.Errorf("Expected key3 to exist")
	}
}

func TestZoneCacheBackgroundCleanup(t *testing.T) {
	cache := NewZoneCacheWithName(10, 50*time.Millisecond, "test-zone")
	defer cache.Stop()

	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 1, Response: true}}

	// Add entry that will expire quickly
	cache.Set("test-key", msg)

	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}

	// Wait for background cleanup (TTL/4 = 12.5ms interval)
	// Entry expires after 50ms, cleanup should happen multiple times
	time.Sleep(100 * time.Millisecond)

	// Entry should be cleaned up by background routine
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after background cleanup, got %d", cache.Size())
	}
}

func BenchmarkCacheGet(b *testing.B) {
	cache := NewZoneCache(1000, 5*time.Minute)
	defer cache.Stop()

	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 1, Response: true}}
	cache.Set("bench-key", msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("bench-key")
	}
}

func BenchmarkCacheSet(b *testing.B) {
	cache := NewZoneCache(1000, 5*time.Minute)
	defer cache.Stop()

	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 1, Response: true}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("bench-key", msg)
	}
}

func BenchmarkMemoryCalculation(b *testing.B) {
	cache := NewZoneCache(1, time.Minute)
	defer cache.Stop()

	testCases := []struct {
		name string
		msg  *dns.Msg
	}{
		{"empty", &dns.Msg{}},
		{"simple_a", createSimpleARecord()},
		{"complex", createComplexDNSMessage()},
		{"txt_record", createTXTRecord()},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			key := "benchmark.test.com:A"
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = cache.calculateEntrySize(key, tc.msg)
			}
		})
	}
}

func BenchmarkDNSMessageSizeCalculation(b *testing.B) {
	cache := NewZoneCache(1, time.Minute)
	defer cache.Stop()

	testCases := []struct {
		name string
		msg  *dns.Msg
	}{
		{"empty", &dns.Msg{}},
		{"simple_a", createSimpleARecord()},
		{"complex", createComplexDNSMessage()},
		{"txt_record", createTXTRecord()},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = cache.calculateDNSMsgSize(tc.msg)
			}
		})
	}
}