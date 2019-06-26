import BaseTable from './BaseTable.jsx';
import CheckCircleOutline from '@material-ui/icons/CheckCircleOutline';
import PropTypes from 'prop-types';
import React from 'react';
import Tooltip from '@material-ui/core/Tooltip';
import WarningIcon from '@material-ui/icons/Warning';
import { directionColumn } from './util/TapUtils.jsx';
import { processedEdgesPropType } from './util/EdgesUtils.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  secure: {
    color: theme.status.dark.good
  },
  warning: {
    color: theme.status.dark.warning
  }
});

const edgesColumnDefinitions = (PrefixedLink, namespace, type, classes) => {
  return [
    {
      title: " ",
      dataIndex: "direction",
      render: d => directionColumn(d.direction),
    },
    {
      title: "Name",
      dataIndex: "name",
      isNumeric: false,
      filter: d => d.name,
      render: d => {
        // check that the resource is a k8s resource with a name we can link to
        if (namespace && type && d.name) {
          return (
            <PrefixedLink to={`/namespaces/${namespace}/${type}s/${d.name}`}>
              {d.name}
            </PrefixedLink>
          );
        } else {
          return d.name;
        }
      },
      sorter: d => d.name
    },
    {
      title: "Identity",
      dataIndex: "identity",
      isNumeric: false,
      filter: d => d.identity,
      render: d => d.identity ? `${d.identity.split('.')[0]}.${d.identity.split('.')[1]}` : null,
      sorter: d => d.identity
    },
    {
      title: "Secured",
      dataIndex: "message",
      isNumeric: true,
      render: d => {
        if (d.noIdentityMsg === "") {
          return <CheckCircleOutline className={classes.secure} />;
        } else {
          return (
            <Tooltip title={d.noIdentityMsg}>
              <WarningIcon className={classes.warning} />
            </Tooltip>
          );
        }
      }
    }
  ];
};

const generateEdgesTableTitle = edges => {
  let title = "Edges";
  if (edges.length > 0) {
    let identity = edges[0].direction === "INBOUND" ? edges[0].serverId : edges[0].clientId;
    identity = identity.split('.')[0] + '.' + identity.split('.')[1];
    title = `${title} (Identity: ${identity})`;
  }
  return title;
};

class EdgesTable extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      prefixedUrl: PropTypes.func.isRequired,
    }).isRequired,
    classes: PropTypes.shape({}).isRequired,
    edges: PropTypes.arrayOf(processedEdgesPropType),
    namespace: PropTypes.string.isRequired,
    type: PropTypes.string.isRequired
  };

  static defaultProps = {
    edges: [],
  };


  render() {
    const { edges, api, namespace, type, classes } = this.props;
    let edgesColumns = edgesColumnDefinitions(api.PrefixedLink, namespace, type, classes);
    let edgesTableTitle = generateEdgesTableTitle(edges);

    return (
      <BaseTable
        defaultOrderBy="name"
        enableFilter={true}
        tableRows={edges}
        tableColumns={edgesColumns}
        tableClassName="metric-table"
        title={edgesTableTitle}
        padding="dense" />
    );
  }
}

export default withStyles(styles)(EdgesTable);
