import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import classNames from 'classnames';
import {Link} from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import Version from './Version.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import {
  AppBar,
  Collapse,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListSubheader,
  Toolbar,
  Typography
} from '@material-ui/core';
import {
  Business as BusinessIcon,
  ChevronLeft as ChevronLeftIcon,
  Dashboard as DashboardIcon,
  ExpandLess,
  ExpandMore,
  Home as HomeIcon,
  LibraryBooks as LibraryBooksIcon,
  Menu as MenuIcon,
  Pageview as PageviewIcon
} from '@material-ui/icons';
import { linkerdWordLogo, navIconTop } from './util/SvgWrappers.jsx';

const drawerWidth = 250;
const styles = theme => ({
  root: {
    flexGrow: 1,
    height: "100vh",
    width: "100%",
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
    width: `calc(100% - ${drawerWidth}px)`,
    backgroundColor: theme.palette.background.default,
    padding: theme.spacing.unit * 3,
  },
});

class NavigationBase extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    // this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      drawerOpen: true,
      resourceMenuOpen: false,
      latestVersion: '',
      isLatest: true,
      namespaceFilter: "all"
    };
  }

  componentDidMount() {
    this.fetchVersion();
  }

  fetchVersion() {
    let versionUrl = `https://versioncheck.linkerd.io/version.json?version=${this.props.releaseVersion}&uuid=${this.props.uuid}&source=web`;
    fetch(versionUrl, { credentials: 'include' })
      .then(rsp => rsp.json())
      .then(versionRsp => {
        let latestVersion;
        let parts = this.props.releaseVersion.split("-", 2);
        if (parts.length === 2) {
          latestVersion = versionRsp[parts[0]];
        }
        this.setState({
          latestVersion,
          isLatest: latestVersion === this.props.releaseVersion
        });
      }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      error: e
    });
  }

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
    const { classes, ChildComponent, api } = this.props;
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

            <Typography variant="h6" color="inherit" noWrap>
              <BreadcrumbHeader {...this.props} />
            </Typography>
          </Toolbar>
        </AppBar>

        <Drawer
          variant="permanent"
          classes={{
            paper: classNames(classes.drawerPaper, !this.state.drawerOpen && classes.drawerPaperClose),
          }}
          open={this.state.drawerOpen}>
          <div className={classes.toolbar}>
            <div className="linkerd-nav-logo">
              {linkerdWordLogo}
            </div>
            <IconButton onClick={this.handleDrawerClose}>
              <ChevronLeftIcon />
            </IconButton>
          </div>

          <Divider />

          <List>
            <ListItem component={Link} to={prefixLink("/overview")}>
              <ListItemIcon><HomeIcon /></ListItemIcon>
              <ListItemText primary="Overview" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/tap")}>
              <ListItemIcon>{navIconTop}</ListItemIcon>
              <ListItemText primary="Tap" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/top")}>
              <ListItemIcon><PageviewIcon /></ListItemIcon>
              <ListItemText primary="Top" />
            </ListItem>
            <ListItem component={Link} to={prefixLink("/servicemesh")}>
              <ListItemIcon><BusinessIcon /></ListItemIcon>
              <ListItemText primary="ServiceMesh" />
            </ListItem>
            <ListItem button onClick={this.handleResourceMenuClick}>
              <ListItemIcon><DashboardIcon /></ListItemIcon>
              <ListItemText inset primary="Resources" />
              {this.state.resourceMenuOpen ? <ExpandLess /> : <ExpandMore />}
            </ListItem>
            <Collapse in={this.state.resourceMenuOpen} timeout="auto" unmountOnExit>
              <List component="div" disablePadding>
                <ListItem component={Link} to={prefixLink("/authorities")} className={classes.nested}>
                  <ListItemIcon><DashboardIcon /></ListItemIcon>
                  <ListItemText primary="Authorities" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/deployments")} className={classes.nested}>
                  <ListItemIcon><DashboardIcon /></ListItemIcon>
                  <ListItemText primary="Deployments" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/namespaces")} className={classes.nested}>
                  <ListItemIcon><DashboardIcon /></ListItemIcon>
                  <ListItemText primary="Namespaces" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/pods")} className={classes.nested}>
                  <ListItemIcon><DashboardIcon /></ListItemIcon>
                  <ListItemText primary="Pods" />
                </ListItem>
                <ListItem component={Link} to={prefixLink("/replicationcontrollers")} className={classes.nested}>
                  <ListItemIcon><DashboardIcon /></ListItemIcon>
                  <ListItemText primary="Replication Controllers" />
                </ListItem>
              </List>
            </Collapse>
          </List>

          <Divider />

          <List
            component="nav"
            subheader={!this.state.drawerOpen ? null : <ListSubheader component="div">Nested List Items</ListSubheader>}>
            <ListItem button>
              <ListItemIcon>
                <DashboardIcon />
              </ListItemIcon>
              <ListItemText inset primary="Sent mail" />
            </ListItem>
            <ListItem button>
              <ListItemIcon>
                <DashboardIcon />
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

          {
            !this.state.drawerOpen ? null : <Version
              isLatest={this.state.isLatest}
              latestVersion={this.state.latestVersion}
              releaseVersion={this.props.releaseVersion}
              error={this.state.error}
              uuid={this.props.uuid} />
          }
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
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  ChildComponent: PropTypes.func.isRequired,
  classes: PropTypes.shape({}).isRequired,
  releaseVersion: PropTypes.string.isRequired,
  theme: PropTypes.shape({}).isRequired,
  uuid: PropTypes.string.isRequired,
};

export default withContext(withStyles(styles, { withTheme: true })(NavigationBase));
