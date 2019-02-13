import BaseTable from './BaseTable.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _reduce from 'lodash/reduce';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import { numericSort } from './util/Utils.js';
import { withContext } from './util/AppContext.jsx';
import withREST from './util/withREST.jsx';

const endpointColumns = [
  {
    title: "Namespace",
    dataIndex: "namespace",
    sorter: (a, b) => (a.namespace).localeCompare(b.namespace)
  },
  {
    title: "IP",
    dataIndex: "pod.podIP"
  },
  {
    title: "Port",
    dataIndex: "addr.port",
    sorter: (a, b) => numericSort(a.addr.port, b.addr.port)
  },
  {
    title: "Pod",
    dataIndex: "name",
    sorter: (a, b) => (a.name).localeCompare(b.name)
  },
  {
    title: "Resource Version",
    dataIndex: "pod.resourceVersion",
    sorter: (a, b) => (a.pod.resourceVersion).localeCompare(b.resourceVersion)
  },
  {
    title: "Service",
    dataIndex: "service",
    sorter: (a, b) => (a.service).localeCompare(b.service)
  }
];
class Endpoints extends React.Component {
  static defaultProps = {
    error: null
  }

  static propTypes = {
    data: PropTypes.arrayOf(PropTypes.shape({})).isRequired,
    error:  apiErrorPropType,
    loading: PropTypes.bool.isRequired,
  }

  banner = () => {
    const { error } = this.props;
    if (!error) {
      return;
    }
    return <ErrorBanner message={error} />;
  }

  loading = () => {
    const { loading } = this.props;
    if (!loading) {
      return;
    }

    return <Spinner />;
  }

  render() {
    const { data } = this.props;
    let results = _get(data, '[0].servicePorts', {});
    let rows = _reduce(results, (mem, svc, svcName) => {
      let pods = [];
      _each(svc.portEndpoints, info => {
        info.podAddresses.forEach(podAddress => {
          podAddress.service = svcName;
          let [podNamespace, podName] = podAddress.pod.name.split("/");
          podAddress.name = podName;
          podAddress.namespace = podNamespace;
          pods.push(podAddress);
        });
      });
      return mem.concat(pods);
    }, []);

    return (
      <React.Fragment>
        {this.loading()}
        {this.banner()}

        <BaseTable
          tableRows={rows}
          tableColumns={endpointColumns}
          tableClassName="metric-table"
          defaultOrderBy="namespace"
          rowKey={r => r.service + r.pod.name}
          padding="dense" />

      </React.Fragment>
    );
  }
}

export default withREST(
  withContext(Endpoints),
  ({api}) => [api.fetch("/api/endpoints")]
);
