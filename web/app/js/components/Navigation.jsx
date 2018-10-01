import React from 'react';
import PropTypes from 'prop-types';
import classNames from 'classnames';
import { withStyles } from '@material-ui/core/styles';
import Drawer from '@material-ui/core/Drawer';
import AppBar from '@material-ui/core/AppBar';
import Button from '@material-ui/core/Button';
import {Link} from 'react-router-dom';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import Badge from '@material-ui/core/Badge';
import Collapse from '@material-ui/core/Collapse';
import MenuItem from '@material-ui/core/MenuItem';
import Menu from '@material-ui/core/Menu';
import Toolbar from '@material-ui/core/Toolbar';
import List from '@material-ui/core/List';
import Typography from '@material-ui/core/Typography';
import Divider from '@material-ui/core/Divider';
import IconButton from '@material-ui/core/IconButton';
import MenuIcon from '@material-ui/icons/Menu';
import ChevronLeftIcon from '@material-ui/icons/ChevronLeft';
import ChevronRightIcon from '@material-ui/icons/ChevronRight';
import { withContext } from './util/AppContext.jsx';
import ListSubheader from '@material-ui/core/ListSubheader';
import { linkerdWordLogo} from './util/SvgWrappers.jsx';
import ListItem from '@material-ui/core/ListItem';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import HomeIcon from '@material-ui/icons/Home';
import CompareArrowsIcon from '@material-ui/icons/CompareArrows';
import WavesIcon from '@material-ui/icons/Waves';
import LibraryBooksIcon from '@material-ui/icons/LibraryBooks';
import NotificationsIcon from '@material-ui/icons/Notifications';

import GroupWorkIcon from  '@material-ui/icons/GroupWork';
import ExpandLess from '@material-ui/icons/ExpandLess';
import ExpandMore from '@material-ui/icons/ExpandMore';

const drawerWidth = 250;
const styles = theme => ({
  root: {
    flexGrow: 1,
    height: "100vh",
    width: "100vw",
    zIndex: 1,
    overflowX: 'scroll',
    position: 'relative',
    display: 'flex',
  },
  appBar: {
    zIndex: theme.zIndex.drawer + 1,
    transition: theme.transitions.create(['width', 'margin'], {
      easing: theme.transitions.easing.sharp,
      duration: theme.transitions.duration.leavingScreen,
    }),
  },
  appBarShift: {
    marginLeft: drawerWidth,
    width: `calc(100% - ${drawerWidth}px)`,
    transition: theme.transitions.create(['width', 'margin'], {
      easing: theme.transitions.easing.sharp,
      duration: theme.transitions.duration.enteringScreen,
    }),
  },
  menuButton: {
    marginLeft: 12,
    marginRight: 36,
  },
  hide: {
    display: 'none',
  },
  drawerPaper: {
    position: 'relative',
    whiteSpace: 'nowrap',
    width: drawerWidth,
    transition: theme.transitions.create('width', {
      easing: theme.transitions.easing.sharp,
      duration: theme.transitions.duration.enteringScreen,
    }),
  },
  drawerPaperClose: {
    overflowX: 'hidden',
    transition: theme.transitions.create('width', {
      easing: theme.transitions.easing.sharp,
      duration: theme.transitions.duration.leavingScreen,
    }),
    width: theme.spacing.unit * 7,
    [theme.breakpoints.up('sm')]: {
      width: theme.spacing.unit * 9,
    },
  },
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'flex-end',
    padding: '0 8px',
    ...theme.mixins.toolbar,
  },
  content: {
    flexGrow: 1,
    backgroundColor: theme.palette.background.default,
    padding: theme.spacing.unit * 3,
  },
});

class NavigationBase extends React.Component {
  state = {
    drawerOpen: false,
    resourceMenuOpen: false
  };

  handleDrawerOpen = () => {
    this.setState({ drawerOpen: true });
  };

  handleDrawerClose = () => {
    this.setState({ drawerOpen: false });
  };

  handleResourceMenuClick = () => {
    this.setState(state => ({ resourceMenuOpen: !state.resourceMenuOpen }));
  };

  render() {
    const { classes, theme, ChildComponent, api } = this.props;
    const prefixLink = api.prefixLink;

    return (
      <div className={classes.root}>
        <AppBar
          position="absolute"
          className={classNames(classes.appBar, this.state.drawerOpen && classes.appBarShift)}>
          <Toolbar disableGutters={!this.state.drawerOpen}>
            <IconButton
              color="inherit"
              aria-label="Open drawer"
              onClick={this.handleDrawerOpen}
              className={classNames(classes.menuButton, this.state.drawerOpen && classes.hide)}>
              <MenuIcon />
            </IconButton>
            <Typography variant="title" color="inherit" noWrap>
              <BreadcrumbHeader {...this.props} />
            </Typography>
            <div className={classes.grow} />
            <IconButton color="inherit">
              <Badge className={classes.margin} badgeContent="1" color="secondary">
                <NotificationsIcon />
              </Badge>
            </IconButton>
          </Toolbar>
        </AppBar>
        <Drawer
          variant="permanent"
          classes={{
            paper: classNames(classes.drawerPaper, !this.state.drawerOpen && classes.drawerPaperClose),
          }}
          open={this.state.drawerOpen}>
          <div className={classes.toolbar}>
            {linkerdWordLogo}
            <IconButton onClick={this.handleDrawerClose}>
              {theme.direction === 'rtl' ? <ChevronRightIcon /> : <ChevronLeftIcon />}
            </IconButton>
          </div>
          <Divider />
          <List>
            <ListItem component={Link} to={prefixLink("/overview")}>
              <ListItemIcon><HomeIcon /></ListItemIcon>
              <ListItemText primary="Overview" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/tap")}>
              <ListItemIcon><CompareArrowsIcon /></ListItemIcon>
              <ListItemText primary="Tap" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/top")}>
              <ListItemIcon><WavesIcon /></ListItemIcon>
              <ListItemText primary="Top" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/servicemesh")}>
              <ListItemIcon><GroupWorkIcon /></ListItemIcon>
              <ListItemText primary="ServiceMesh" />
            </ListItem>
            <ListItem button onClick={this.handleResourceMenuClick}>
              <ListItemIcon><WavesIcon /></ListItemIcon>
              <ListItemText inset primary="Resources" />
              {this.state.resourceMenuOpen ? <ExpandLess /> : <ExpandMore />}
            </ListItem>
            <Collapse in={this.state.resourceMenuOpen} timeout="auto" unmountOnExit>
              <List component="div" disablePadding>
                <ListItem component={Link} to={prefixLink("/authorities")} className={classes.nested}>
                  <ListItemIcon><GroupWorkIcon /></ListItemIcon>
                  <ListItemText primary="Authorities" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/deployments")} className={classes.nested}>
                  <ListItemIcon><GroupWorkIcon /></ListItemIcon>
                  <ListItemText primary="Deployments" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/namespaces")} className={classes.nested}>
                  <ListItemIcon><GroupWorkIcon /></ListItemIcon>
                  <ListItemText primary="Namespaces" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/pods")} className={classes.nested}>
                  <ListItemIcon><GroupWorkIcon /></ListItemIcon>
                  <ListItemText primary="Pods" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/replicationcontrollers")} className={classes.nested}>
                  <ListItemIcon><GroupWorkIcon /></ListItemIcon>
                  <ListItemText primary="Replication Controllers" />
                </ListItem>
              </List>
            </Collapse>
          </List>
          <Divider />
          <List
            component="nav"
            subheader={<ListSubheader component="div">Nested List Items</ListSubheader>}>
            <ListItem button>
              <ListItemIcon>
                <WavesIcon />
              </ListItemIcon>
              <ListItemText inset primary="Sent mail" />
            </ListItem>
            <ListItem button>
              <ListItemIcon>
                <WavesIcon />
              </ListItemIcon>
              <ListItemText inset primary="Drafts" />
            </ListItem>
          </List>
          <List>
            <ListItem component={Link} to="https://linkerd.io/2/overview/" target="_blank">
              <ListItemIcon><LibraryBooksIcon /></ListItemIcon>
              <ListItemText primary="Documentation" />
            </ListItem>
          </List>
        </Drawer>
        <main className={classes.content}>
          <div className={classes.toolbar} />
          <div className="main-content"><ChildComponent {...this.props} /></div>
        </main>
      </div>
    );
  }
}

NavigationBase.propTypes = {
  classes: PropTypes.object.isRequired,
  theme: PropTypes.object.isRequired,
};

export default withContext(withStyles(styles, { withTheme: true })(NavigationBase));
