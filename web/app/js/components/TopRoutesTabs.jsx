import AppBar from '@material-ui/core/AppBar';
import ConfigureProfilesMsg from './ConfigureProfilesMsg.jsx';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Tab from '@material-ui/core/Tab';
import Tabs from '@material-ui/core/Tabs';
import TopModule from './TopModule.jsx';
import TopRoutesModule from './TopRoutesModule.jsx';
import _isEmpty from 'lodash/isEmpty';
import _noop from 'lodash/noop';
import { withStyles } from '@material-ui/core/styles';
import { withTranslation } from 'react-i18next';

const styles = theme => ({
  root: {
    flexGrow: 1,
    backgroundColor: theme.palette.background.paper,
    marginBottom: theme.spacing.unit * 3,
  },
});

class TopRoutesTabs extends React.Component {
  state = {
    value: 0,
  };

  handleChange = (_event, value) => {
    this.setState({ value });
  };

  renderTopComponent() {
    let { disableTop, query, pathPrefix, updateUnmeshedSources } = this.props;
    if (disableTop) {
      return null;
    }

    let topQuery = {
      resource: query.resourceType + "/" + query.resourceName,
      namespace: query.namespace
    };

    return (
      <React.Fragment>
        <TopModule
          pathPrefix={pathPrefix}
          query={topQuery}
          startTap={true}
          updateUnmeshedSources={updateUnmeshedSources}
          maxRowsToDisplay={10} />
        <QueryToCliCmd cmdName="top" query={topQuery} resource={topQuery.resource} />
      </React.Fragment>
    );
  }

  renderRoutesComponent() {
    const { query } = this.props;

    if (_isEmpty(query)) {
      return <ConfigureProfilesMsg />;
    }

    let routesQuery = {
      resource_name: query.resourceName,
      resource_type: query.resourceType,
      namespace: query.namespace
    };
    let resource = query.resourceType + "/" + query.resourceName;

    return (
      <React.Fragment>
        <TopRoutesModule query={routesQuery} />
        <QueryToCliCmd cmdName="routes" query={routesQuery} resource={resource} />
      </React.Fragment>
    );
  }

  render() {
    const { classes, t } = this.props;
    const { value } = this.state;

    return (
      <Paper className={classes.root}>
        <AppBar position="static" className={classes.root}>

          <Tabs
            value={value}
            onChange={this.handleChange}
            indicatorColor="primary"
            textColor="primary">
            <Tab label={t("Live Calls")} />
            <Tab label={t("Route Metrics")} />
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
  disableTop: PropTypes.bool,
  pathPrefix: PropTypes.string.isRequired,
  query: PropTypes.shape({}),
  t: PropTypes.func.isRequired,
  theme: PropTypes.shape({}).isRequired,
  updateUnmeshedSources: PropTypes.func
};

TopRoutesTabs.defaultProps = {
  disableTop: false,
  query: {},
  updateUnmeshedSources: _noop
};

export default withTranslation()(withStyles(styles, { withTheme: true })(TopRoutesTabs));
