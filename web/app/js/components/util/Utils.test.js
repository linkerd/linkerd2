import {
  formatLatencySec,
  metricToFormatter,
  regexFilterString,
  styleNum,
  toClassName
} from './Utils.js';

// introduce some binary floating point rounding errors, like ya do
function float(num) {
  return num * 0.1 * 10;
}

describe('Utils', () => {
  describe('styleNum', () => {
    it('properly formats numbers', () => {
      let compare = (f, s) => expect(styleNum(float(f))).toEqual(s);
      compare(1, "1");
      compare(2.20, "2.2");
      compare(3, "3");
      compare(4.4, "4.4");
      compare(5.0000001, "5");
      compare(7.6666667, "7.67");
      compare(123.456, "123.46");
      compare(1212.999999, "1.213k");
      compare(5329.333333, "5.329k");
      compare(16384.888, "16.385k");
      compare(131042, "131.042k");
      compare(1048576, "1.049M");
      compare(2097152.1, "2.097M");
      compare(16777216, "16.777M");
      compare(536870912, "536.871M");
      compare(1073741824, "1.074G");
      compare(68719476736, "68.719G");
    });

    it('properly formats numbers with units and no truncation', () => {
      let compare = (f, s) => expect(styleNum(float(f), " RPS", false)).toEqual(s);
      compare(1, "1 RPS");
      compare(2.20, "2.2 RPS");
      compare(3, "3 RPS");
      compare(4.4, "4.4 RPS");
      compare(5.0000001, "5 RPS");
      compare(7.6666667, "7.67 RPS");
      compare(123.456, "123.46 RPS");
      compare(1212.999999, "1,213 RPS");
      compare(5329.333333, "5,329 RPS");
      compare(16384.888, "16,385 RPS");
      compare(131042, "131,042 RPS");
      compare(1048576, "1,048,576 RPS");
      compare(2097152.1, "2,097,152 RPS");
      compare(16777216, "16,777,216 RPS");
      compare(536870912, "536,870,912 RPS");
      compare(1073741824, "1,073,741,824 RPS");
      compare(68719476736, "68,719,476,736 RPS");
    });
  });

  describe('Metric Formatters', () => {
    it('formats undefined input', () => {
      let undefinedMetric;
      expect(metricToFormatter["REQUEST_RATE"](undefinedMetric)).toEqual('---');
      expect(metricToFormatter["SUCCESS_RATE"](undefinedMetric)).toEqual('---');
      expect(metricToFormatter["LATENCY"](undefinedMetric)).toEqual('---');
    });

    it('formats requests with rounding and unit', () => {
      expect(metricToFormatter["REQUEST_RATE"](99)).toEqual('99 RPS');
      expect(metricToFormatter["REQUEST_RATE"](999)).toEqual('999 RPS');
      expect(metricToFormatter["REQUEST_RATE"](1000)).toEqual('1k RPS');
      expect(metricToFormatter["REQUEST_RATE"](4444)).toEqual('4.444k RPS');
      expect(metricToFormatter["REQUEST_RATE"](9999)).toEqual('9.999k RPS');
      expect(metricToFormatter["REQUEST_RATE"](99999)).toEqual('99.999k RPS');
    });

    it('formats subsecond latency as ms', () => {
      expect(metricToFormatter["LATENCY"](99)).toEqual('99 ms');
      expect(metricToFormatter["LATENCY"](999)).toEqual('999 ms');
    });

    it('formats latency greater than 1s as s', () => {
      expect(metricToFormatter["LATENCY"](1000)).toEqual('1.00 s');
      expect(metricToFormatter["LATENCY"](9999)).toEqual('10.0 s');
      expect(metricToFormatter["LATENCY"](99999)).toEqual('100 s');
    });

    it('formats success rate', () => {
      expect(metricToFormatter["SUCCESS_RATE"](0.012345)).toEqual('1.23%');
      expect(metricToFormatter["SUCCESS_RATE"](0.01)).toEqual('1.00%');
      expect(metricToFormatter["SUCCESS_RATE"](0.1)).toEqual('10.00%');
      expect(metricToFormatter["SUCCESS_RATE"](0.9999)).toEqual('99.99%');
      expect(metricToFormatter["SUCCESS_RATE"](4)).toEqual('400.00%');
    });

    it('formats bytes', () => {
      expect(metricToFormatter["BYTES"](123.938112312)).toEqual('123.94B/s');
      expect(metricToFormatter["BYTES"](1234.32)).toEqual('1.234kB/s');
      expect(metricToFormatter["BYTES"](12345.1831)).toEqual('12.345kB/s');
      expect(metricToFormatter["BYTES"](1234567.02384)).toEqual('1.235MB/s');
    });

    it('formats latencies expressed as seconds into a more appropriate display unit', () => {
      expect(formatLatencySec("0.002837700")).toEqual("3 ms");
      expect(formatLatencySec("0.000")).toEqual("0 s");
      expect(formatLatencySec("0.000000797")).toEqual("1 µs");
      expect(formatLatencySec("0.000231910")).toEqual("232 µs");
      expect(formatLatencySec("0.000988600")).toEqual("989 µs");
      expect(formatLatencySec("0.005598200")).toEqual("6 ms");
      expect(formatLatencySec("3.029409200")).toEqual("3.03 s");
      expect(formatLatencySec("34.395600")).toEqual("34.4 s");
    });
  });

  describe('toClassName', () => {
    it('converts a string to a valid class name', () => {
      expect(toClassName('')).toEqual('');
      expect(toClassName('---')).toEqual('');
      expect(toClassName('foo/bar/baz')).toEqual('foo_bar_baz');
      expect(toClassName('FOOBAR')).toEqual('foobar');
      expect(toClassName('FooBar')).toEqual('foo_bar');

      // the perhaps unexpected number of spaces here are due to the fact that
      // _.lowerCase returns space separated words
      expect(toClassName('potato123yam0squash')).toEqual('potato_123_yam_0_squash');
      expect(toClassName('test/potato-e1af21-f3f3')).toEqual('test_potato_e_1_af_21_f_3_f_3');
    });
  });

  describe('regexFilterString', () => {
    it('converts input string to a valid regex for filtering', () => {
      expect(regexFilterString('emojivoto')).toEqual(new RegExp(/emojivoto/));
      expect(regexFilterString('emojivoto123')).toEqual(new RegExp(/emojivoto123/));
      expect(regexFilterString('emojivoto*')).toEqual(new RegExp(/emojivoto.+/));
      expect(regexFilterString('emojivoto**')).toEqual(new RegExp(/emojivoto.+.+/));
      expect(regexFilterString('Emojivoto')).toEqual(new RegExp(/emojivoto/));
      expect(regexFilterString('emojivoto{}')).toEqual(new RegExp(/emojivoto/));
      expect(regexFilterString('emojivoto_.')).toEqual(new RegExp(/emojivoto_./));
      expect(regexFilterString('emojivoto_.{')).toEqual(new RegExp(/emojivoto_./));
      expect(regexFilterString('emojivoto//')).toEqual(new RegExp(/emojivoto\/\//));
      expect(regexFilterString('emojivoto//')).toEqual(new RegExp(/emojivoto\/\//));
      expect(regexFilterString('emojivoto-prod-1')).toEqual(new RegExp(/emojivoto-prod-1/));
      expect(regexFilterString('emoji??Voto-pr##od-1')).toEqual(new RegExp(/emojivoto-prod-1/));
    })
  });
});
