import AppBar from '@material-ui/core/AppBar';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Tab from '@material-ui/core/Tab';
import Tabs from '@material-ui/core/Tabs';
import TopModule from './TopModule.jsx';
import TopRoutesModule from './TopRoutesModule.jsx';
import _ from 'lodash';
import withREST from './util/withREST.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    flexGrow: 1,
    backgroundColor: theme.palette.background.paper,
  },
});

class TopRoutesTabs extends React.Component {
  state = {
    value: 0,
  };

  handleChange = (_event, value) => {
    this.setState({ value });
  };

  handleChangeIndex = index => {
    this.setState({ value: index });
  };

  renderTopComponent() {
    let { disableTop, query, pathPrefix, updateNeighborsFromTapData } = this.props;
    if (disableTop) {
      return null;
    }

    let topQuery = {
      resource: query.resourceType + "/" + query.resourceName,
      namespace: query.namespace
    };

    return (
      <React.Fragment>
        <QueryToCliCmd cmdName="top" query={topQuery} resource={topQuery.resource} />
        <TopModule
          pathPrefix={pathPrefix}
          query={topQuery}
          startTap={true}
          updateNeighbors={updateNeighborsFromTapData}
          maxRowsToDisplay={10} />
      </React.Fragment>
    );
  }

  renderRoutesComponent() {
    const { data, query } = this.props;

    if (_.isEmpty(query)) {
      return null;
    }

    let servicesInThisNs = _.get(data, "[0].services", []);
    let serviceToQuery = _.find(servicesInThisNs, s => query.resourceName.indexOf(s.name) !== -1);

    if (_.isNil(serviceToQuery)) {
      return null;
    }

    let routesQueryWithFrom = {
      resource_name: serviceToQuery.name,
      namespace: query.namespace,
      from_type: query.resourceType,
      from_namespace: query.namespace,
      from: query.resourceType
    };

    return (
      <React.Fragment>
        <QueryToCliCmd cmdName="routes" query={routesQueryWithFrom} resource={routesQueryWithFrom.resource_name} />
        <TopRoutesModule query={routesQueryWithFrom} />
      </React.Fragment>
    );
  }

  render() {
    const { classes } = this.props;
    const { value } = this.state;

    return (
      <Paper className={classes.root}>
        <AppBar position="static" className={classes.root}>

          <Tabs
            value={value}
            onChange={this.handleChange}
            indicatorColor="primary"
            textColor="primary">
            <Tab label="Sampled Route Data" />
            <Tab label="Live Route Data" />
          </Tabs>
        </AppBar>
        {value === 0 && this.renderTopComponent()}
        {value === 1 && this.renderRoutesComponent()}
      </Paper>
    );
  }
}

TopRoutesTabs.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  data: PropTypes.arrayOf(PropTypes.shape({})),
  disableTop: PropTypes.bool,
  pathPrefix: PropTypes.string.isRequired,
  query: PropTypes.shape({}),
  theme: PropTypes.shape({}).isRequired,
  updateNeighborsFromTapData: PropTypes.func
};

TopRoutesTabs.defaultProps = {
  data: [],
  disableTop: false,
  query: {},
  updateNeighborsFromTapData: _.noop
};

export default withREST(
  withStyles(styles, { withTheme: true })(TopRoutesTabs),
  ({api, query}) => {
    return [api.fetchServices(query.namespace)];
  },
  {
    resetProps: ["query.resourceName"],
  },
);
