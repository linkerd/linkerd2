import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import ErrorModal from './ErrorModal.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Tooltip from '@material-ui/core/Tooltip';
import { withContext } from './util/AppContext.jsx';

const getClassification = (meshedPodCount, failedPodCount) => {
  if (failedPodCount > 0) {
    return "poor";
  } else if (meshedPodCount === 0) {
    return "neutral";
  } else {
    return "good";
  }
};

const namespacesColumns = PrefixedLink => [
  {
    title: "Namespace",
    key: "namespace",
    render: d => {
      return  (
        <React.Fragment>
          <PrefixedLink to={"/namespaces/" + d.namespace}>{d.namespace}</PrefixedLink>
          { _.isEmpty(d.errors) ? null :
          <ErrorModal errors={d.errors} resourceName={d.namespace} resourceType="namespace" />
          }
        </React.Fragment>
      );
    }
  },
  {
    title: "Meshed pods",
    key: "meshedPodsStr",
    isNumeric: true,
    render: d => d.meshedPodsStr
  },
  {
    title: "Meshed Status",
    key: "meshification",
    render: row => {
      let containerWidth = 132;
      let percent = row.meshedPercent.get();
      let barWidth = percent < 0 ? 0 : Math.round(percent * containerWidth);
      let barType = _.isEmpty(row.errors) ?
        getClassification(row.meshedPods, row.failedPods) : "poor";


      let percentMeshedMsg = "";
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
          <div className={"container-bar " + barType} style={{width: containerWidth}}>
            <div className={"inner-bar " + barType} style={{width: barWidth}}>&nbsp;</div>
          </div>
        </Tooltip>
      );
    }
  }
];

class MeshedStatusTable extends React.Component {
  render() {
    return (
      <BaseTable
        tableClassName="metric-table mesh-completion-table"
        tableRows={this.props.tableRows}
        tableColumns={namespacesColumns(this.props.api.PrefixedLink)}
        rowKey={d => d.namespace} />
    );
  }
}

MeshedStatusTable.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired
  }).isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({}))
};

MeshedStatusTable.defaultProps = {
  tableRows: []
};

export default withContext(MeshedStatusTable);
