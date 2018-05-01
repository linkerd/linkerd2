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
          name: 'voting',
          namespace: 'emojivoto',
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
      expect(result[0].name).to.equal("emoji");
      expect(result[0].namespace).to.equal("emojivoto");
      expect(result[1].name).to.equal("vote-bot");
      expect(result[1].namespace).to.equal("emojivoto");
      expect(result[2].name).to.equal("voting");
      expect(result[2].namespace).to.equal("emojivoto");
      expect(result[3].name).to.equal("web");
      expect(result[3].namespace).to.equal("emojivoto");
    });
  });
});
