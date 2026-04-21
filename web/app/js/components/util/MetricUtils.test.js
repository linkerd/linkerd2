import deployRollupFixtures from '../../../test/fixtures/deployRollup.json';
import multiDeployRollupFixtures from '../../../test/fixtures/multiDeployRollup.json';
import multiResourceRollupFixtures from '../../../test/fixtures/allRollup.json';
import gatewayFixtures from '../../../test/fixtures/gateway.json';
import Percentage from './Percentage';
import {
  processGatewayResults,
  processMultiResourceRollup,
  processSingleResourceRollup
} from './MetricUtils.jsx';

describe('MetricUtils', () => {
  describe('processSingleResourceRollup', () => {
    it('Extracts deploy metrics from a single response', () => {
      let result = processSingleResourceRollup(deployRollupFixtures);
      let expectedResult = [
        {
          name: 'voting',
          namespace: 'emojivoto',
          type: 'deployment',
          key: "emojivoto-deployment-voting",
          requestRate: 2.5,
          successRate: 0.9,
          tcp: {
            openConnections: 221,
            readBytes: 4421,
            writeBytes: 4421,
            readRate: 73.68333333333334,
            writeRate: 73.68333333333334
          },
          totalRequests: 150,
          latency: {
            P50: 1,
            P95: 2,
            P99: 7
          },
          pods: { totalPods: "1", meshedPods: "1", meshedPercentage: new Percentage(1, 1) },
          added: true,
          errors: {}
        }
      ];
      expect(result).toHaveLength(1);
      expect(result).toEqual(expectedResult);
    });

    it('Extracts and sorts multiple deploys from a single response', () => {
      let result = processSingleResourceRollup(multiDeployRollupFixtures);
      expect(result).toHaveLength(4);
      expect(result[0].name).toEqual("emoji");
      expect(result[0].namespace).toEqual("emojivoto");
      expect(result[1].name).toEqual("vote-bot");
      expect(result[1].namespace).toEqual("emojivoto");
      expect(result[2].name).toEqual("voting");
      expect(result[2].namespace).toEqual("emojivoto");
      expect(result[3].name).toEqual("web");
      expect(result[3].namespace).toEqual("emojivoto");
    });
  });

  describe('processMultiResourceRollup', () => {
    it('Extracts metrics and groups them by resource type', () => {
      let result = processMultiResourceRollup(multiResourceRollupFixtures);
      expect(Object.keys(result)).toHaveLength(2);

      expect(result["deployment"]).toHaveLength(1);
      expect(result["pod"]).toHaveLength(4);
      expect(result["replicationcontroller"]).toBeUndefined;
    });
  });
  describe('processGatewayResults', () => {
    it('Extracts and sorts gateway metrics from a response', () => {
      let result = processGatewayResults(gatewayFixtures);
      let expectedResult = [
        {
          key: 'test_namespace-gateway-default_gateway',
          name: 'default_gateway',
          namespace: 'test_namespace',
          clusterName: 'multi_cluster',
          alive: true,
          pairedServices: '0',
          latency: { P50: 0, P95: 0, P99: 0 }
        },
        {
          key: 'test_namespace-gateway-test_gateway',
          name: 'test_gateway',
          namespace: 'test_namespace',
          clusterName: 'multi_cluster',
          alive: true,
          pairedServices: '0',
          latency: { P50: 0, P95: 0, P99: 0 }
        }
      ];
      expect(result).toHaveLength(2);
      expect(result).toEqual(expectedResult);
    });
  })
});
