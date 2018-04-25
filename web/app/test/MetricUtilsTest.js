import deployRollupFixtures from './fixtures/deployRollup.json';
import { expect } from 'chai';
import multiDeployRollupFixtures from './fixtures/multiDeployRollup.json';
import { processRollupMetrics } from '../js/components/util/MetricUtils.js';

describe('MetricUtils', () => {
  describe('processRollupMetrics', () => {
    it('Extracts deploy metrics from a single response', () => {
      let result = processRollupMetrics(deployRollupFixtures);
      let expectedResult = [
        {
          name: 'emojivoto/voting',
          requestRate: 2.5,
          successRate: 0.9,
          latency: {
            P50: 1,
            P95: 2,
            P99: 7
          },
          added: true
        }
      ];
      expect(result).to.deep.equal(expectedResult);
    });

    it('Extracts and sorts multiple deploys from a single response', () => {
      let result = processRollupMetrics(multiDeployRollupFixtures);
      expect(result).to.have.length(4);
      expect(result[0].name).to.equal("emojivoto/emoji");
      expect(result[1].name).to.equal("emojivoto/vote-bot");
      expect(result[2].name).to.equal("emojivoto/voting");
      expect(result[3].name).to.equal("emojivoto/web");
    });
  });
});
