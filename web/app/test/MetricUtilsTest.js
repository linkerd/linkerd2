import _ from 'lodash';
import deployRollupFixtures from './fixtures/deployRollup.json';
import { expect } from 'chai';
import multiDeployRollupFixtures from './fixtures/multiDeployRollup.json';
import multiResourceRollupFixtures from './fixtures/allRollup.json';
import Percentage from '../js/components/util/Percentage';
import {
  processMultiResourceRollup,
  processSingleResourceRollup
} from '../js/components/util/MetricUtils.js';

describe('MetricUtils', () => {
  describe('processSingleResourceRollup', () => {
    it('Extracts deploy metrics from a single response', () => {
      let result = processSingleResourceRollup(deployRollupFixtures);
      let expectedResult = [
        {
          name: 'voting',
          namespace: 'emojivoto',
          requestRate: 2.5,
          successRate: 0.9,
          totalRequests: 150,
          tlsRequestPercent: new Percentage(100, 150),
          latency: {
            P50: 1,
            P95: 2,
            P99: 7
          },
          added: true
        }
      ];
      expect(result).to.have.length(1);
      expect(result[0].tlsRequestPercent.prettyRate()).to.equal("66.7%");
      expect(result).to.deep.equal(expectedResult);
    });

    it('Extracts and sorts multiple deploys from a single response', () => {
      let result = processSingleResourceRollup(multiDeployRollupFixtures);
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

  describe('processMultiResourceRollup', () => {
    it('Extracts metrics and groups them by resource type', () => {
      let result = processMultiResourceRollup(multiResourceRollupFixtures);
      expect(_.size(result)).to.equal(2);

      expect(result["deployments"]).to.have.length(1);
      expect(result["pods"]).to.have.length(4);
      expect(result["replicationcontrollers"]).to.be.undefined;
    });
  });
});
