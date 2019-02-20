import BaseTable from './BaseTable.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import { Link } from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import Spinner from './util/Spinner.jsx';
import Typography from '@material-ui/core/Typography';
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
    render: d => <Link to={"/namespaces/" + d.namespace}>{d.namespace}</Link>,
    sorter: (a, b) => (a.namespace).localeCompare(b.namespace)
  },
  {
    title: "IP",
    dataIndex: "ip"
  },
  {
    title: "Port",
    dataIndex: "port",
    sorter: (a, b) => numericSort(a.port, b.port)
  },
  {
    title: "Pod",
    dataIndex: "name",
    sorter: (a, b) => (a.name).localeCompare(b.name),
    render: d => <Link to={`/namespaces/${d.namespace}/pods/${d.name}`}>{d.name}</Link>
  },
  {
    title: "Resource Version",
    dataIndex: "resourceVersion",
    sorter: (a, b) => numericSort(a.resourceVersion, b.resourceVersion)
  },
  {
    title: "Service",
    dataIndex: "service",
    sorter: (a, b) => (a.service).localeCompare(b.service)
  }
];
class Debug extends React.Component {
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
          let [podNamespace, podName] = podAddress.pod.name.split("/");

          pods.push({
            service: svcName,
            name: podName,
            namespace: podNamespace,
            resourceVersion: parseInt(podAddress.pod.resourceVersion, 10),
            ip: podAddress.pod.podIP,
            port: podAddress.addr.port,
          });
        });
      });
      return mem.concat(pods);
    }, []);

    return (
      <React.Fragment>
        {this.loading()}
        {this.banner()}
        <Typography variant="h6">Endpoints</Typography>
        <Typography>
        This table allow you to see Linkerd&#39;s service discovery state. It provides
        debug information about the internal state of the
        control-plane&#39;s proxy-api container. Note that this cache of service discovery
        information is populated on-demand via linkerd-proxy requests. No endpoints
        will be found  until a linkerd-proxy begins routing
        requests.
        </Typography>

        <BaseTable
          tableRows={rows}
          tableColumns={endpointColumns}
          tableClassName="metric-table"
          defaultOrderBy="namespace"
          rowKey={r => r.service + r.name}
          padding="dense" />

      </React.Fragment>
    );
  }
}

export default withREST(
  withContext(Debug),
  ({api}) => [api.fetch("/api/endpoints")]
);
