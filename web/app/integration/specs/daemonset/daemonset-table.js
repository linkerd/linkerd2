const assert = require('assert');

describe('table check', function() {
  it('should check the page for proper table structure', () => {
    browser.url('http://127.0.0.1:7777/daemonsets');
    browser.waitUntil(() => {
      return $('table').isExisting();
    }, 10000, 'expected table to exist');
    httpHeaders=["Namespace", "Daemon Set","Meshed", "Success Rate", "RPS", "P50 Latency","P95 Latency", "P99 Latency", "Grafana"];
    tcpHeaders=["Namespace","Daemon Set","Meshed","Connections", "Read Bytes / sec","Write Bytes / sec","Grafana"];
    const pageTables=$$("table");
    const httpTable=pageTables[0].$("thead");
    const tcpTable=pageTables[1].$("thead");
    const http=httpTable.$$("th").map(item=>item.getText());
    assert(http.join('')===httpHeaders.join(''));
    const tcp=tcpTable.$$("th").map(item=>item.getText());
    assert(tcp.join('')===tcpHeaders.join(''));
  });
});
