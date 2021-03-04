import AppBar from '@material-ui/core/AppBar';
import ConfigureProfilesMsg from './ConfigureProfilesMsg.jsx';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Tab from '@material-ui/core/Tab';
import Tabs from '@material-ui/core/Tabs';
import TapEnabledWarning from './TapEnabledWarning.jsx';
import TopModule from './TopModule.jsx';
import TopRoutesModule from './TopRoutesModule.jsx';
import { Trans } from '@lingui/macro';
import _isEmpty from 'lodash/isEmpty';
import _noop from 'lodash/noop';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    flexGrow: 1,
    backgroundColor: theme.palette.background.paper,
    marginBottom: theme.spacing(3),
  },
});

class TopRoutesTabs extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      value: 0,
    };
  }

  handleChange = (_, value) => {
    this.setState({ value });
  };

  renderTopComponent() {
    const { disableTop, query, pathPrefix, updateUnmeshedSources } = this.props;
    if (disableTop) {
      return null;
    }

    const topQuery = {
      resource: `${query.resourceType}/${query.resourceName}`,
      namespace: query.namespace,
    };

    return (
      <React.Fragment>
        <TopModule
          pathPrefix={pathPrefix}
          query={topQuery}
          startTap
          updateUnmeshedSources={updateUnmeshedSources}
          maxRowsToDisplay={10}
          tapEnabledWarningComponent={<TapEnabledWarning
            resource={topQuery.resource}
            namespace={topQuery.namespace} />} />
        <QueryToCliCmd cmdName="top" query={topQuery} resource={topQuery.resource} />
      </React.Fragment>
    );
  }

  renderRoutesComponent() {
    const { query } = this.props;

    if (_isEmpty(query)) {
      return <ConfigureProfilesMsg />;
    }

    const routesQuery = {
      resource_name: query.resourceName,
      resource_type: query.resourceType,
      namespace: query.namespace,
    };
    const resource = `${query.resourceType}/${query.resourceName}`;

    return (
      <React.Fragment>
        <TopRoutesModule query={routesQuery} />
        <QueryToCliCmd cmdName="routes" query={routesQuery} resource={resource} />
      </React.Fragment>
    );
  }

  render() {
    const { classes } = this.props;
    const { value } = this.state;

    return (
      <Paper className={classes.root} elevation={3}>
        <AppBar position="static" className={classes.root}>

          <Tabs
            value={value}
            onChange={this.handleChange}
            indicatorColor="primary"
            textColor="primary">
            <Tab label={<Trans>tabLiveCalls</Trans>} />
            <Tab label={<Trans>tabRouteMetrics</Trans>} />
          </Tabs>
        </AppBar>
        {value === 0 && this.renderTopComponent()}
        {value === 1 && this.renderRoutesComponent()}
      </Paper>
    );
  }
}

TopRoutesTabs.propTypes = {
  disableTop: PropTypes.bool,
  pathPrefix: PropTypes.string.isRequired,
  query: PropTypes.shape({
    namespace: PropTypes.string,
    resourceType: PropTypes.string,
    resourceName: PropTypes.string,
  }),
  theme: PropTypes.shape({}).isRequired,
  updateUnmeshedSources: PropTypes.func,
};

TopRoutesTabs.defaultProps = {
  disableTop: false,
  query: {},
  updateUnmeshedSources: _noop,
};

export default withStyles(styles, { withTheme: true })(TopRoutesTabs);
