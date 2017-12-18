import _ from 'lodash';
import deployRollupFixtures from './fixtures/deployRollup.json';
import { expect } from 'chai';
import multiDeployRollupFixtures from './fixtures/multiDeployRollup.json';
import timeseriesFixtures from './fixtures/singleDeployTs.json';
import { processRollupMetrics, processTimeseriesMetrics } from '../js/components/util/MetricUtils.js';

describe('MetricUtils', () => {
  describe('processTsWithLatencyBreakdown', () => {
    it('Converts raw metrics to plottable timeseries data', () => {
      let deployName = 'test/potato3';
      let histograms = ['P50', 'P95', 'P99'];
      let result = processTimeseriesMetrics(timeseriesFixtures.metrics, "targetDeploy")[deployName];

      _.each(histograms, quantile => {
        _.each(result["LATENCY"][quantile], datum => {
          expect(datum.timestamp).not.to.be.empty;
          expect(datum.value).not.to.be.empty;
          expect(datum.label).to.equal(quantile);
        });
      });

      _.each(result["REQUEST_RATE"], datum => {
        expect(datum.timestamp).not.to.be.empty;
        expect(datum.value).to.exist;
      });

      _.each(result["SUCCESS_RATE"], datum => {
        expect(datum.timestamp).not.to.be.empty;
        expect(datum.value).to.exist;
      });
    });
  });

  describe('processMetrics', () => {
    it('Extracts the values from the nested raw rollup response', () => {
      let result = processRollupMetrics(deployRollupFixtures.metrics, "targetDeploy");
      let expectedResult = [
        {
          name: 'test/potato3',
          requestRate: 6.1,
          successRate: 0.3770491803278688,
          latency: {
            P95: [ { label: 'P95', value: '953' } ],
            P99: [ { label: 'P99', value: '990' } ],
            P50: [ { label: 'P50', value: '537' } ],
          },
          added: true
        }
      ];
      expect(result).to.deep.equal(expectedResult);
    });

    it('Extracts the specified entity metrics in the rollup', () => {
      let helloResult = processRollupMetrics(multiDeployRollupFixtures.metrics, "targetDeploy");
      let helloPodResult = processRollupMetrics(multiDeployRollupFixtures.metrics, "targetPod");
      let meshResult = processRollupMetrics(multiDeployRollupFixtures.metrics, "component");
      let pathResult = processRollupMetrics(multiDeployRollupFixtures.metrics, "path");

      expect(helloResult).to.have.length(1);
      expect(helloPodResult).to.have.length(1);
      expect(meshResult).to.have.length(1);
      expect(pathResult).to.have.length(1);

      expect(helloResult[0].name).to.equal("default/hello");
      expect(helloPodResult[0].name).to.equal("default/hello-12f3f-1e2aa");
      expect(meshResult[0].name).to.equal("mesh");
      expect(pathResult[0].name).to.equal("/Get/Hello");
    });
  });
});
