import Collapse from '@material-ui/core/Collapse';
import ExpandMore from '@material-ui/icons/ExpandMore';
import { Link } from 'react-router-dom';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import MenuItem from '@material-ui/core/MenuItem';
import MenuList from '@material-ui/core/MenuList';
import NavigateNextIcon from '@material-ui/icons/NavigateNext';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateDot from "./util/SuccessRateDot.jsx";
import _each from 'lodash/each';
import _includes from 'lodash/includes';
import _merge from 'lodash/merge';
import _orderBy from 'lodash/orderBy';
import { friendlyTitle } from "./util/Utils.js";
import { getAggregatedTrafficSplitMetrics } from './TrafficSplitDetail.jsx';
import { processedMetricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  navMenuItem: {
    paddingLeft: "24px",
    paddingRight: "12px",
  },
  navResourceItem: {
    marginRight: "0",
    paddingLeft: "38px",
  },
  navResourceText: {
    overflow: "hidden",
    padding: "0px 0px 0px 10px",
    textOverflow: "ellipsis",
  }
});

// the Stat API returns a row for each leaf within a trafficsplit. we should
// list each trafficsplit only once in the sidebar, and calculate the aggregated
// success rate so that the success rate dot is the correct color.
const getTrafficSplitResourceList = metrics => {
  let splitsByName = {},
    splitListToDisplay = [];
  _each(metrics, row => {
    if (!_includes(splitsByName, row.name)) {
      splitsByName[row.name] = [row];
    } else {
      splitsByName[row.name].push(row);
    }
  });
  _each(splitsByName, split => {
    let metrics = getAggregatedTrafficSplitMetrics(split);
    let name = split[0].name;
    let namespace = split[0].namespace;
    splitListToDisplay.push({
      name: name,
      type: "trafficsplit",
      successRate: metrics.successRate,
      namespace: namespace,
      menuName: `${namespace}/${name}`});
  });
  return _orderBy(splitListToDisplay, split => split.menuName);
};

class NavigationResource extends React.Component {
  static defaultProps = {
    metrics: [],
  }

  static propTypes = {
    api: PropTypes.shape({
      prefixLink: PropTypes.func.isRequired,
    }).isRequired,
    classes: PropTypes.shape({}).isRequired,
    metrics: PropTypes.arrayOf(processedMetricsPropType),
    type: PropTypes.string.isRequired,
  };

  constructor(props) {
    super(props);
    this.to = props.api.prefixLink("/" + props.type);
    this.state = {open: false};
  }

  handleOnClick = () => {
    this.setState({ open: !this.state.open });
  };

  listItemIcon() {
    let icon = <NavigateNextIcon />;
    if (this.state.open) {
      icon = <ExpandMore />;
    }
    return (<ListItemIcon>{icon}</ListItemIcon>);
  }

  menu() {
    const { classes, type } = this.props;

    return (
      <MenuItem
        className={classes.navMenuItem}
        onClick={this.handleOnClick}>
        {this.listItemIcon()}
        <ListItemText primary={friendlyTitle(type).plural} />
      </MenuItem>
    );
  }

  subMenu() {
    const { api, classes, metrics, type } = this.props;
    let resources;

    if (type === "trafficsplits") {
      resources = getTrafficSplitResourceList(metrics);
    } else {
      resources = _orderBy(metrics
        .filter(m => m.pods.meshedPods !== "0")
        .map(m =>
          _merge(m, {
            menuName: this.props.type === "namespaces" ? m.name : `${m.namespace}/${m.name}`
          })
        ), r => r.menuName);
    }

    return (
      <MenuList dense component="div" disablePadding>
        <MenuItem
          component={Link}
          to={this.to}
          className={classes.navMenuItem}
          selected={this.to === window.location.pathname}>
          <ListItemIcon className={classes.navResourceItem}>
            <SuccessRateDot />
          </ListItemIcon>
          <ListItemText
            disableTypography
            primary="All"
            className={classes.navResourceText} />
        </MenuItem>
        {
          resources.map(r => {
            let url = api.prefixLink(api.generateResourceURL(r));
            return (
              <MenuItem
                component={Link}
                to={url}
                key={url}
                className={classes.navMenuItem}
                selected={url === window.location.pathname}>
                <ListItemIcon className={classes.navResourceItem}>
                  <SuccessRateDot sr={r.successRate} />
                </ListItemIcon>
                <ListItemText
                  disableTypography
                  primary={r.menuName}
                  className={classes.navResourceText} />
              </MenuItem>
            );
          })
        }
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

export default withContext(withStyles(styles, { withTheme: true })(NavigationResource));
