import _ from 'lodash';
import ErrorModal from './ErrorModal.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import { friendlyTitle, metricToFormatter } from './util/Utils.js';
import { processedMetricsPropType, successRateWithMiniChart } from './util/MetricUtils.jsx';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing.unit * 3,
    overflowX: 'auto',
  },
  table: {
    minWidth: 700,
  },
});

const columnDefinitions = (resource, showNamespaceColumn, PrefixedLink) => {
  let isAuthorityTable = resource === "authority";

  let nsColumn = [
    {
      title: "Namespace",
      key: "namespace",
      isNumeric: false,
      render: d => !d.namespace ? "---" : <PrefixedLink to={"/namespaces/" + d.namespace}>{d.namespace}</PrefixedLink>
    }
  ];

  let grafanaLinkColumn = [
    {
      title: "Grafana Dashboard",
      key: "grafanaDashboard",
      isNumeric: true,
      render: row => !row.added || _.get(row, "pods.totalPods") === "0" ? null : (
        <GrafanaLink
          name={row.name}
          namespace={row.namespace}
          resource={resource}
          PrefixedLink={PrefixedLink} />
      )
    }
  ];

  let meshedColumn = {
    title: "Meshed",
    key: "meshed",
    isNumeric: true,
    render: d => d.pods.meshedPods + "/" + d.pods.totalPods
  };

  let columns = [
    {
      title: friendlyTitle(resource).singular,
      key: "resource-title",
      isNumeric: false,
      render: d => {
        let nameContents;
        if (resource === "namespace") {
          nameContents = <PrefixedLink to={"/namespaces/" + d.name}>{d.name}</PrefixedLink>;
        } else if (!d.added || isAuthorityTable) {
          nameContents = d.name;
        } else {
          nameContents = (
            <PrefixedLink to={"/namespaces/" + d.namespace + "/" + resource + "s/" + d.name}>
              {d.name}
            </PrefixedLink>
          );
        }
        return (
          <React.Fragment>
            {nameContents}
            { _.isEmpty(d.errors) ? null : <ErrorModal errors={d.errors} resourceName={d.name} resourceType={resource} /> }
          </React.Fragment>
        );
      }
    },
    {
      title: "Success Rate",
      key: "success-rate",
      isNumeric: true,
      render: d => successRateWithMiniChart(d.successRate)
    },
    {
      title: "Request Rate",
      key: "request-rate",
      isNumeric: true,
      render: d => metricToFormatter["NO_UNIT"](d.requestRate)
    },
    {
      title: "P50 Latency",
      key: "p50_latency",
      isNumeric: true,
      render: d => metricToFormatter["LATENCY"](d.P50)
    },
    {
      title: "P95 Latency",
      key: "p95_latency",
      isNumeric: true,
      render: d => metricToFormatter["LATENCY"](d.P95)
    },
    {
      title: "P99 Latency",
      key: "p99_latency",
      isNumeric: true,
      render: d => metricToFormatter["LATENCY"](d.P99)
    },
    {
      title: "TLS",
      key: "has_tls",
      isNumeric: true,
      render: d => _.isNil(d.tlsRequestPercent) || d.tlsRequestPercent.get() === -1 ? "---" : d.tlsRequestPercent.prettyRate()
    }
  ];

    // don't add the meshed column on a Authority MetricsTable
  if (!isAuthorityTable) {
    columns.splice(1, 0, meshedColumn);
    columns = _.concat(columns, grafanaLinkColumn);
  }

  if (!showNamespaceColumn) {
    return columns;
  } else {
    return _.concat(nsColumn, columns);
  }
};


const preprocessMetrics = metrics => {
  let tableData = _.cloneDeep(metrics);

  _.each(tableData, datum => {
    _.each(datum.latency, (value, quantile) => {
      datum[quantile] = value;
    });
  });

  return tableData;
};

function MetricTable(props) {
  const { classes, metrics, resource, showNamespaceColumn, api } = props;

  let showNsColumn = resource === "namespace" ? false : showNamespaceColumn;

  let columns = columnDefinitions(resource, showNsColumn, api.PrefixedLink);
  let rows = preprocessMetrics(metrics);

  return (
    <Paper className={classes.root}>
      <Table className={`${classes.table} metric-table`}>
        <TableHead>
          <TableRow>
            { _.map(columns, c => (
              <TableCell
                key={c.key}
                numeric={c.isNumeric}>{c.title}
              </TableCell>
            ))
            }
          </TableRow>
        </TableHead>
        <TableBody>
          {
            _.map(rows, d => {
            return (
              <TableRow key={d.key}>
                { _.map(columns, c => (
                  <TableCell
                    key={`table-${d.key}-${c.key}`}
                    numeric={c.isNumeric}>{c.render(d)}
                  </TableCell>
                  ))
                }
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </Paper>
  );
}

MetricTable.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  classes: PropTypes.shape({}).isRequired,
  metrics: PropTypes.arrayOf(processedMetricsPropType),
  resource: PropTypes.string.isRequired,
  showNamespaceColumn: PropTypes.bool
};
MetricTable.defaultProps = {
  showNamespaceColumn: true,
  metrics: []
};

export default withContext(withStyles(styles)(MetricTable));
