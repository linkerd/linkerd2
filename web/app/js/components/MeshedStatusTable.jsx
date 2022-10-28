import BaseTable from './BaseTable.jsx';
import ErrorModal from './ErrorModal.jsx';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { StyledProgress } from './util/Progress.jsx';
import Tooltip from '@material-ui/core/Tooltip';
import { Trans } from '@lingui/macro';
import _isEmpty from 'lodash/isEmpty';
import { withContext } from './util/AppContext.jsx';
import { Link } from 'react-router-dom';

const getClassification = (meshedPodCount, failedPodCount) => {
  if (failedPodCount > 0) {
    return 'poor';
  } else if (meshedPodCount === 0) {
    return 'default';
  } else {
    return 'good';
  }
};

const namespacesColumns = () => [
  {
    title: <Trans>columnTitleNamespace</Trans>,
    dataIndex: 'namespace',
    sorter: d => d.namespace,
    render: d => {
      return (
        <Grid container alignItems="center" spacing={1}>
          <Grid item><Link to={`/namespaces/${d.namespace}`}>{d.namespace}</Link></Grid>
          { _isEmpty(d.errors) ? null :
          <Grid item><ErrorModal errors={d.errors} resourceName={d.namespace} resourceType="namespace" /></Grid>
          }
        </Grid>
      );
    },
  },
  {
    title: <Trans>columnTitleMeshedPods</Trans>,
    dataIndex: 'meshedPodsStr',
    isNumeric: true,
  },
  {
    title: <Trans>columnTitleMeshedStatus</Trans>,
    key: 'meshification',
    render: row => {
      const percent = row.meshedPercent.get();
      const barType = _isEmpty(row.errors) ?
        getClassification(row.meshedPods, row.failedPods) : 'warning';
      const Progress = StyledProgress(barType);

      let percentMeshedMsg = '';
      if (row.meshedPercent.get() >= 0) {
        percentMeshedMsg = `(${row.meshedPercent.prettyRate()})`;
      }
      return (
        <Tooltip
          title={(
            <div>
              <div>
                {`${row.meshedPods} out of ${row.totalPods} running or pending pods are in the mesh ${percentMeshedMsg}`}
              </div>
              {row.failedPods === 0 ? null : <div>{ `${row.failedPods} failed pods` }</div>}
            </div>
            )}>
          <Progress variant="determinate" value={Math.round(percent * 100)} />
        </Tooltip>
      );
    },
  },
];

const MeshedStatusTable = function({ tableRows }) {
  return (
    <BaseTable
      tableClassName="metric-table mesh-completion-table"
      tableRows={tableRows}
      tableColumns={namespacesColumns()}
      defaultOrderBy="namespace"
      rowKey={d => d.namespace} />
  );
};

MeshedStatusTable.propTypes = {
  tableRows: PropTypes.arrayOf(PropTypes.shape({})),
};

MeshedStatusTable.defaultProps = {
  tableRows: [],
};

export default withContext(MeshedStatusTable);
