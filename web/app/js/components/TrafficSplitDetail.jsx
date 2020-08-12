import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _each from 'lodash/each';
import _isNil from 'lodash/isNil';
import _reduce from 'lodash/reduce';
import { processSingleResourceRollup } from './util/MetricUtils.jsx';

// calculates the aggregated successRate and RPS for an entire trafficsplit
export const getAggregatedTrafficSplitMetrics = resourceMetrics => {
  let totalRPS = 0;
  const successRates = [];
  _each(resourceMetrics, row => {
    if (!_isNil(row.requestRate)) {
      totalRPS += row.requestRate;
    }
    if (!_isNil(row.successRate)) {
      const weightedSuccessRate = row.successRate * row.requestRate;
      successRates.push(weightedSuccessRate);
    }
  });
  const sumSuccessRates = _reduce(successRates, (acc, n) => {
    return acc + n;
  }, 0);
  const aggregatedSuccessRate = sumSuccessRates / totalRPS || 0;
  return { successRate: aggregatedSuccessRate,
    totalRPS };
};

const generateApexMetrics = resourceMetrics => {
  const aggregatedMetrics = getAggregatedTrafficSplitMetrics(resourceMetrics);
  return {
    name: resourceMetrics[0].tsStats.apex,
    type: 'service',
    requestRate: aggregatedMetrics.totalRPS,
    successRate: aggregatedMetrics.successRate,
    isApexService: true,
  };
};

// the Stat API returns a row for each leaf; however, to be consistent with
// other resources, row.name is the trafficsplit name while row.tsStats.leaf is
// the leaf name. here we replace the trafficsplit name with the leaf name for
// the octopus graph.
const formatLeaves = resourceRsp => {
  const leaves = processSingleResourceRollup(resourceRsp, 'trafficsplit');
  _each(leaves, leaf => {
    leaf.name = leaf.tsStats.leaf;
    leaf.type = 'service';
    leaf.isLeafService = true;
  });
  return leaves;
};

const TrafficSplitDetail = ({ resourceMetrics, resourceName, resourceRsp, resourceType }) => {
  const apexResource = generateApexMetrics(resourceMetrics);

  return (
    <div>
      <Grid container justify="space-between" alignItems="center">
        <Grid item><Typography variant="h5">{resourceType}/{resourceName}</Typography></Grid>
      </Grid>

      <Octopus
        resource={apexResource}
        neighbors={{ upstream: [], downstream: formatLeaves(resourceRsp) }} />

      <MetricsTable
        resource="trafficsplit"
        metrics={resourceMetrics}
        showName={false}
        showNamespaceColumn={false}
        title={<Trans>tableTitleLeafServices</Trans>} />
    </div>
  );
};

TrafficSplitDetail.propTypes = {
  resourceMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
  resourceName: PropTypes.string.isRequired,
  resourceRsp: PropTypes.shape({}).isRequired,
  resourceType: PropTypes.string.isRequired,
};

export default TrafficSplitDetail;
