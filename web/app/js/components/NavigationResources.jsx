import { metricsPropType, processMultiResourceRollup } from './util/MetricUtils.jsx';
import Collapse from '@material-ui/core/Collapse';
import ExpandLess from '@material-ui/icons/ExpandLess';
import ExpandMore from '@material-ui/icons/ExpandMore';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import MenuItem from '@material-ui/core/MenuItem';
import MenuList from '@material-ui/core/MenuList';
import NavigationResource from './NavigationResource.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import ViewListIcon from '@material-ui/icons/ViewList';
import _has from 'lodash/has';
import { withContext } from './util/AppContext.jsx';
import withREST from './util/withREST.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  navMenuItem: {
    paddingLeft: "24px",
    paddingRight: "24px",
  },
});

class NavigationResourcesBase extends React.Component {
  static propTypes = {
    classes: PropTypes.shape({}).isRequired,
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
  }

  constructor(props) {
    super(props);
    this.state = { open: false };
  }

  handleOnClick = () => {
    this.setState({ open: !this.state.open });
  };

  menu() {
    const { classes } = this.props;

    return (
      <MenuItem
        className={classes.navMenuItem}
        button
        onClick={this.handleOnClick}>
        <ListItemIcon><ViewListIcon /></ListItemIcon>
        <ListItemText inset primary="Resources" />
        {this.state.open ? <ExpandLess /> : <ExpandMore />}
      </MenuItem>
    );
  }

  subMenu() {
    const { data } = this.props;

    let allMetrics = {};
    let nsMetrics = {};
    if (_has(data, '[0]')) {
      allMetrics = processMultiResourceRollup(data[0]);

      if (_has(data, '[1]')) {
        nsMetrics = processMultiResourceRollup(data[1]);
      }
    }

    // the "authorities" type is displayed as "services", so below list is alphabetical
    return (
      <MenuList dense component="div" disablePadding>
        <NavigationResource type="deployments" metrics={allMetrics.deployment} />
        <NavigationResource type="daemonsets" metrics={allMetrics.daemonset} />
        <NavigationResource type="jobs" metrics={allMetrics.job} />
        <NavigationResource type="namespaces" metrics={nsMetrics.namespace} />
        <NavigationResource type="pods" metrics={allMetrics.pod} />
        <NavigationResource type="replicationcontrollers" metrics={allMetrics.replicationcontroller} />
        <NavigationResource type="authorities" />
        <NavigationResource type="statefulsets" metrics={allMetrics.statefulset} />
      </MenuList>
    );
  }

  render() {
    return (
      <React.Fragment>
        {this.menu()}
        <Collapse in={this.state.open} timeout="auto" unmountOnExit>
          {this.subMenu()}
        </Collapse>
      </React.Fragment>
    );
  }
}

export default withREST(
  withContext(withStyles(styles, { withTheme: true })(NavigationResourcesBase)),
  ({ api }) => [
    // TODO: modify "all" to also retrieve namespaces, also share fetch with parent component
    api.fetchMetrics(api.urlsForResource("all")),
    api.fetchMetrics(api.urlsForResource("namespace")),
  ],
  {
    resetProps: ['resource'],
  },
);
