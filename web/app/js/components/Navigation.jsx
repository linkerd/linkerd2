import { githubIcon, linkerdWordLogo, slackIcon } from './util/SvgWrappers.jsx';
import AppBar from '@material-ui/core/AppBar';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import ChevronLeftIcon from '@material-ui/icons/ChevronLeft';
import CloudQueueIcon from '@material-ui/icons/CloudQueue';
import Collapse from '@material-ui/core/Collapse';
import Divider from '@material-ui/core/Divider';
import Drawer from '@material-ui/core/Drawer';
import EmailIcon from '@material-ui/icons/Email';
import ExpandLess from '@material-ui/icons/ExpandLess';
import ExpandMore from '@material-ui/icons/ExpandMore';
import HelpIcon from '@material-ui/icons/HelpOutline';
import HomeIcon from '@material-ui/icons/Home';
import Icon from '@material-ui/core/Icon';
import IconButton from '@material-ui/core/IconButton';
import LibraryBooksIcon from '@material-ui/icons/LibraryBooks';
import { Link } from 'react-router-dom';
import ListItem from '@material-ui/core/ListItem';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import MenuIcon from '@material-ui/icons/Menu';
import MenuItem from '@material-ui/core/MenuItem';
import MenuList from '@material-ui/core/MenuList';
import NavigationResources from './NavigationResources.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import Toolbar from '@material-ui/core/Toolbar';
import Typography from '@material-ui/core/Typography';
import Version from './Version.jsx';
import classNames from 'classnames';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => {
  const drawerWidth = theme.spacing.unit * 31;
  const drawerWidthClosed = theme.spacing.unit * 9;
  const navLogoWidth = theme.spacing.unit * 22.5;
  const contentPadding = theme.spacing.unit * 3;

  const enteringFn = prop => theme.transitions.create(prop, {
    easing: theme.transitions.easing.sharp,
    duration: theme.transitions.duration.enteringScreen,
  });
  const leavingFn = prop => theme.transitions.create(prop, {
    easing: theme.transitions.easing.sharp,
    duration: theme.transitions.duration.leavingScreen,
  });

  const entering = enteringFn('width');
  const leaving = leavingFn('width');

  return {
    root: {
      display: 'flex',
    },
    appBar: {
      position: "absolute",
      width: `calc(100% - ${drawerWidthClosed}px)`,
      color: 'white',
      transition: leaving,
    },
    appBarShift: {
      width: `calc(100% - ${drawerWidth}px)`,
      transition: entering,
    },
    drawer: {
      width: drawerWidth,
      transition: entering,
    },
    drawerClose: {
      width: drawerWidthClosed,
      transition: leaving,
    },
    drawerPaper: {
      overflowX: 'hidden',
      whiteSpace: 'nowrap',
      width: drawerWidth,
      transition: entering,
    },
    drawerPaperClose: {
      transition: leaving,
      width: drawerWidthClosed,
      [theme.breakpoints.up('sm')]: {
        width: drawerWidthClosed,
      },
    },
    toolbar: theme.mixins.toolbar,
    navToolbar: {
      display: 'flex',
      alignItems: 'center',
      padding: `0 0 0 ${theme.spacing.unit*2}px`,
      boxShadow: theme.shadows[4], // to match elevation == 4 on main AppBar
      ...theme.mixins.toolbar,
      backgroundColor: theme.palette.primary.main,
    },
    content: {
      flexGrow: 1,
      width: `calc(100% - ${drawerWidth}px)`,
      backgroundColor: theme.palette.background.default,
      padding: contentPadding,
      transition: entering,
    },
    contentDrawerClose: {
      width: `calc(100% - ${drawerWidthClosed}px)`,
      transition: leaving,
    },
    linkerdNavLogo: {
      minWidth: `${navLogoWidth}px`,
      transition: enteringFn(['margin', 'opacity']),
    },
    linkerdNavLogoClose: {
      opacity: "0",
      marginLeft: `-${navLogoWidth+theme.spacing.unit/2}px`,
      transition: leavingFn(['margin', 'opacity']),
    },
    navMenuItem: {
      paddingLeft: `${contentPadding}px`,
      paddingRight: `${contentPadding}px`,
    },
    shrinkIcon: {
      fontSize: "18px",
      paddingLeft: "3px",
    }
  };
};

class NavigationBase extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      drawerOpen: true,
      helpMenuOpen: false,
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
    this.versionPromise = fetch(versionUrl, { credentials: 'include' })
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

  handleDrawerClick = () => {
    this.setState(state => ({ drawerOpen: !state.drawerOpen }));
  };

  handleHelpMenuClick = () => {
    this.setState(state => ({ helpMenuOpen: !state.helpMenuOpen }));
  }

  menuItem(path, title, icon) {
    const { classes, api } = this.props;
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");
    let isCurrentPage = path => path === normalizedPath;

    return (
      <MenuItem
        component={Link}
        to={api.prefixLink(path)}
        className={classes.navMenuItem}
        selected={isCurrentPage(path)}>
        <ListItemIcon>{icon}</ListItemIcon>
        <ListItemText primary={title} />
      </MenuItem>
    );
  }
  render() {
    const { classes, ChildComponent, ...otherProps } = this.props;

    return (
      <div className={classes.root}>
        <AppBar
          className={classNames(classes.appBar, {[classes.appBarShift]: this.state.drawerOpen} )}>
          <Toolbar>
            <Typography variant="h6" color="inherit" noWrap>
              <BreadcrumbHeader {...this.props} />
            </Typography>
          </Toolbar>
        </AppBar>

        <Drawer
          className={classNames(classes.drawer, {[classes.drawerClose]: !this.state.drawerOpen} )}
          variant="permanent"
          classes={{
            paper: classNames(classes.drawerPaper, {[classes.drawerPaperClose]: !this.state.drawerOpen} ),
          }}
          open={this.state.drawerOpen}>
          <div className={classNames(classes.navToolbar)}>
            <div className={classNames(classes.linkerdNavLogo, {[classes.linkerdNavLogoClose]: !this.state.drawerOpen} )}>
              <Link to="/overview">{linkerdWordLogo}</Link>
            </div>
            <IconButton className="drawer-toggle-btn" onClick={this.handleDrawerClick}>
              {this.state.drawerOpen ? <ChevronLeftIcon /> : <MenuIcon />}
            </IconButton>
          </div>

          <Divider />

          <MenuList>
            { this.menuItem("/overview", "Overview", <HomeIcon />) }
            { this.menuItem("/tap", "Tap", <Icon className={classNames("fas fa-microscope", classes.shrinkIcon)} />) }
            { this.menuItem("/top", "Top", <Icon className={classNames("fas fa-stream", classes.shrinkIcon)} />) }
            { this.menuItem("/routes", "Top Routes", <Icon className={classNames("fas fa-random", classes.shrinkIcon)} />) }
            { this.menuItem("/servicemesh", "Service Mesh", <CloudQueueIcon className={classes.shrinkIcon} />) }
            <NavigationResources />
          </MenuList>

          <Divider />

          <MenuList>
            <ListItem component="a" href="https://linkerd.io/2/overview/" target="_blank">
              <ListItemIcon><LibraryBooksIcon /></ListItemIcon>
              <ListItemText primary="Documentation" />
            </ListItem>

            <MenuItem
              className={classes.navMenuItem}
              button
              onClick={this.handleHelpMenuClick}>
              <ListItemIcon><HelpIcon /></ListItemIcon>
              <ListItemText inset primary="Help" />
              {this.state.helpMenuOpen ? <ExpandLess /> : <ExpandMore />}
            </MenuItem>
            <Collapse in={this.state.helpMenuOpen} timeout="auto" unmountOnExit>
              <MenuList dense component="div" disablePadding>
                <ListItem component="a" href="https://lists.cncf.io/g/cncf-linkerd-users" target="_blank">
                  <ListItemIcon><EmailIcon /></ListItemIcon>
                  <ListItemText primary="Join the mailing list" />
                </ListItem>

                <ListItem component="a" href="https://slack.linkerd.io" target="_blank">
                  <ListItemIcon>{slackIcon}</ListItemIcon>
                  <ListItemText primary="Join us on slack" />
                </ListItem>

                <ListItem component="a" href="https://github.com/linkerd/linkerd2/issues/new/choose" target="_blank">
                  <ListItemIcon>{githubIcon}</ListItemIcon>
                  <ListItemText primary="File an issue" />
                </ListItem>
              </MenuList>
            </Collapse>
          </MenuList>

          {
            !this.state.drawerOpen ? null : <Version
              isLatest={this.state.isLatest}
              latestVersion={this.state.latestVersion}
              releaseVersion={this.props.releaseVersion}
              error={this.state.error}
              uuid={this.props.uuid} />
          }
        </Drawer>

        <main className={classNames(classes.content, {[classes.contentDrawerClose]: !this.state.drawerOpen})}>
          <div className={classes.toolbar} />
          <div className="main-content"><ChildComponent {...otherProps} /></div>
        </main>
      </div>
    );
  }
}

NavigationBase.propTypes = {
  api: PropTypes.shape({}).isRequired,
  ChildComponent: PropTypes.func.isRequired,
  classes: PropTypes.shape({}).isRequired,
  location: ReactRouterPropTypes.location.isRequired,
  pathPrefix: PropTypes.string.isRequired,
  releaseVersion: PropTypes.string.isRequired,
  theme: PropTypes.shape({}).isRequired,
  uuid: PropTypes.string.isRequired,
};

export default withContext(withStyles(styles, { withTheme: true })(NavigationBase));
