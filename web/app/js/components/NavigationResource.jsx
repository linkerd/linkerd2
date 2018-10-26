import {
  Collapse,
  IconButton,
  ListItemIcon,
  ListItemText,
  MenuItem,
  MenuList,
} from '@material-ui/core';

import ExpandMore from '@material-ui/icons/ExpandMore';
import { Link } from 'react-router-dom';
import NavigateNextIcon from '@material-ui/icons/NavigateNext';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateDot from "./util/SuccessRateDot.jsx";
import ViewListIcon from '@material-ui/icons/ViewList';
import _ from 'lodash';
import { friendlyTitle } from "./util/Utils.js";
import { processedMetricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  navMenuItem: {
    paddingLeft: "24px",
    "&:hover $navMenuButton": {
      opacity: "1",
    },
  },
  navMenuIcon: {
    marginRight: "0",
  },
  navMenuButton: {
    padding: "4px",
    opacity: "0",
    transition: theme.transitions.create('opacity', {
      duration: theme.transitions.duration.complex,
    }),
  },
  navMenuText: {
    paddingLeft: "0",
  },
  navResourceItem: {
    marginRight: "16px",
    paddingLeft: "33px",
  },
  navResourceText: {
    overflow: "hidden",
    padding: "0",
    textOverflow: "ellipsis",
  }
});

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

    this.resources = _(props.metrics)
      .filter(m => m.pods.meshedPods !== "0")
      .map(m =>
        _.merge(m, {
          menuName: props.type === "namespaces" ? m.name : `${m.namespace}/${m.name}`
        })
      )
      .sortBy("menuName")
      .value();
    this.to = props.api.prefixLink("/" + props.type);

    this.state = {open: false};
  }

  handleOnClick = () => {
    if (!_.isEmpty(this.props.metrics)) {
      // not expandable if no metrics
      this.setState({ open: !this.state.open });
    }
  };

  handleNavButtonClick = e => {
    e.stopPropagation();
  };

  listItemIcon() {
    const { classes } = this.props;

    let icon = <NavigateNextIcon color="disabled" />;
    if (!_.isEmpty(this.resources)) {
      if (this.state.open) {
        icon = <ExpandMore />;
      } else {
        icon = <NavigateNextIcon />;
      }
    }
    return (<ListItemIcon className={classes.navMenuIcon}>{icon}</ListItemIcon>);
  }

  menu() {
    const { classes, type } = this.props;

    return (
      <MenuItem
        className={classes.navMenuItem}
        onClick={this.handleOnClick}
        selected={this.to === window.location.pathname}>
        {this.listItemIcon(this.resources)}
        <IconButton
          component={Link}
          to={this.to}
          className={classes.navMenuButton}
          onClick={this.handleNavButtonClick}>
          <ViewListIcon />
        </IconButton>
        <ListItemText
          className={classes.navMenuText}
          primary={friendlyTitle(type).plural} />
      </MenuItem>
    );
  }

  subMenu() {
    const { api, classes } = this.props;

    return (
      <MenuList dense component="div" disablePadding>
        {
          _.map(this.resources, r => {
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
