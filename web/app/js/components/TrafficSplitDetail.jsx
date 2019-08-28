import Grid from '@material-ui/core/Grid';
import MetricsTable from './MetricsTable.jsx';
import Octopus from './Octopus.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import _each from 'lodash/each';
import _isNil from 'lodash/isNil';
import { processSingleResourceRollup } from './util/MetricUtils.jsx';

// aggregates metrics across all leaves
const generateApexResource = resourceMetrics => {
  let leafNum = resourceMetrics.length;
  let apexMetrics = {
    name: "",
    type: "trafficsplit",
    latency: {P99: null},
    requestRate: null,
    successRate: null
  };
  _each(resourceMetrics, leafRow => {
    if (!_isNil(leafRow.latency.P99)) {apexMetrics.latency.P99+= leafRow.latency.P99/leafNum;}
    if (!_isNil(leafRow.successRate)) {apexMetrics.successRate+= leafRow.successRate/leafNum;}
    if (!_isNil(leafRow.requestRate)) {apexMetrics.requestRate+= leafRow.requestRate/leafNum;}
    if (!_isNil(leafRow.tsStats.apex)) {apexMetrics.name = leafRow.tsStats.apex;}
  });
  return apexMetrics;
};

// the Stat API returns a row for each leaf; however, to be consistent with
// other resources, row.name is the trafficsplit name while row.tsStats.leaf is
// the leaf name. here we replace the trafficsplit name with the leaf name for
// the octopus graph.
const formatLeaves = resourceRsp => {
  let leaves = processSingleResourceRollup(resourceRsp, "trafficsplit");
  _each(leaves, leaf => {
    leaf.name = leaf.tsStats.leaf;
  });
  return leaves;
};
export default class TrafficSplitDetail extends React.Component {
    static propTypes = {
      resourceMetrics: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
      resourceName: PropTypes.string.isRequired,
      resourceRsp: PropTypes.shape({}).isRequired,
      resourceType: PropTypes.string.isRequired,
    };

    render() {
      const { resourceMetrics, resourceName, resourceRsp, resourceType } = this.props;
      const apexResource = generateApexResource(resourceMetrics, resourceName);

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
            title="Leaves" />
        </div>
      );
    }
}
