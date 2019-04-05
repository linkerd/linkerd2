const assert = require('assert');
    describe('table check', function() {
    it('should check the page for proper table structure', () => {
        browser.url('http://127.0.0.1:7777/statefulsets');
        browser.waitUntil(() => {return $('table').isExisting();
          }, 10000, 'expected table to exist');
        http_headers=["Namespace", "Stateful Set","Meshed", "Success Rate", "RPS", "P50 Latency", 
        "P95 Latency", "P99 Latency", "TLS", "Grafana"];
        tcp_headers=["Namespace","Stateful Set","Meshed","Connections", "Read Bytes / sec","Write Bytes / sec", "Grafana"];
        const http=$$("table")[0].$("thead").$$("th").map(item=>item.getText());
        console.log("http headers:"+http);
        assert(http.join('')==http_headers.join(''));
        const tcp=$$("table")[1].$("thead").$$("th").map(item=>item.getText());
        console.log("tcp headers:"+tcp);
        assert(tcp.join('')==tcp_headers.join(''));
    
    });
});