import { githubIcon, linkerdWordLogo, slackIcon } from './util/SvgWrappers.jsx';
import AppBar from '@material-ui/core/AppBar';
import Badge from '@material-ui/core/Badge';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import ChevronLeftIcon from '@material-ui/icons/ChevronLeft';
import CloudQueueIcon from '@material-ui/icons/CloudQueue';
import Divider from '@material-ui/core/Divider';
import Drawer from '@material-ui/core/Drawer';
import EmailIcon from '@material-ui/icons/Email';
import HomeIcon from '@material-ui/icons/Home';
import Icon from '@material-ui/core/Icon';
import IconButton from '@material-ui/core/IconButton';
import LibraryBooksIcon from '@material-ui/icons/LibraryBooks';
import { Link } from 'react-router-dom';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import MenuIcon from '@material-ui/icons/Menu';
import MenuItem from '@material-ui/core/MenuItem';
import MenuList from '@material-ui/core/MenuList';
import NavigationResources from './NavigationResources.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import SentimentVerySatisfiedIcon from '@material-ui/icons/SentimentVerySatisfied';
import Toolbar from '@material-ui/core/Toolbar';
import Typography from '@material-ui/core/Typography';
import Version from './Version.jsx';
import _maxBy from 'lodash/maxBy';
import classNames from 'classnames';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import yellow from '@material-ui/core/colors/yellow';

const jsonFeedUrl = "https://linkerd.io/dashboard/index.json";
const localStorageKey = "linkerd-updates-last-clicked";
const minBrowserWidth = 960;

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
      paddingRight: "3px",
    },
    badge: {
      backgroundColor: yellow[500],
    }
  };
};

class NavigationBase extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.handleCommunityClick = this.handleCommunityClick.bind(this);
    this.updateWindowDimensions = this.updateWindowDimensions.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      drawerOpen: true,
      helpMenuOpen: false,
      hideUpdateBadge: true,
      latestVersion: '',
      isLatest: true,
      namespaceFilter: "all"
    };
  }

  componentDidMount() {
    this.fetchVersion();
    this.fetchLatestCommunityUpdate();
    this.updateWindowDimensions();
    window.addEventListener("resize", this.updateWindowDimensions);
  }

  componentWillUnmount() {
    window.removeEventListener("resize", this.updateWindowDimensions);
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

  fetchLatestCommunityUpdate() {
    this.communityUpdatesPromise = fetch(jsonFeedUrl)
      .then(rsp => rsp.json())
      .then(rsp => rsp.data.date)
      .then(rsp => {
        if (rsp.length > 0) {
          let lastClicked = localStorage[localStorageKey];
          if (!lastClicked) {
            this.setState({ hideUpdateBadge: false });
          } else {
            let lastClickedDateObject = new Date(lastClicked);
            let latestArticle = _maxBy(rsp, update => update.date);
            let latestArticleDateObject = new Date(latestArticle);
            if (latestArticleDateObject > lastClickedDateObject) {
              this.setState({ hideUpdateBadge: false });
            }
          }
        }
      }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      error: e
    });
  }

  handleCommunityClick = () => {
    let lastClicked = new Date();
    localStorage.setItem(localStorageKey, lastClicked);
    this.setState({ hideUpdateBadge: true });
  }

  handleDrawerClick = () => {
    this.setState(state => ({ drawerOpen: !state.drawerOpen }));
  };

  handleHelpMenuClick = () => {
    this.setState(state => ({ helpMenuOpen: !state.helpMenuOpen }));
  }

  menuItem(path, title, icon, onClick) {
    const { classes, api } = this.props;
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");
    let isCurrentPage = path => path === normalizedPath;

    return (
      <MenuItem
        component={Link}
        onClick={onClick}
        to={api.prefixLink(path)}
        className={classes.navMenuItem}
        selected={isCurrentPage(path)}>
        <ListItemIcon>{icon}</ListItemIcon>
        <ListItemText primary={title} />
      </MenuItem>
    );
  }

  updateWindowDimensions() {
    let browserWidth = window.innerWidth;
    if (browserWidth < minBrowserWidth) {
      this.setState({ drawerOpen: false });
    } else {
      this.setState({ drawerOpen: true });
    }
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
            <MenuItem component="a" href="https://linkerd.io/2/overview/" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon><LibraryBooksIcon className={classes.shrinkIcon} /></ListItemIcon>
              <ListItemText primary="Documentation" />
            </MenuItem>
            { this.menuItem("/community", "Community",
              <Badge
                classes={{ badge: classes.badge }}
                invisible={this.state.hideUpdateBadge}
                badgeContent="1">
                <SentimentVerySatisfiedIcon className={classes.shrinkIcon} />
              </Badge>, this.handleCommunityClick
              ) }
            <MenuItem component="a" href="https://lists.cncf.io/g/cncf-linkerd-users" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon><EmailIcon className={classes.shrinkIcon} /></ListItemIcon>
              <ListItemText primary="Join the Mailing List" />
            </MenuItem>

            <MenuItem component="a" href="https://slack.linkerd.io" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon>{slackIcon}</ListItemIcon>
              <ListItemText primary="Join us on Slack" />
            </MenuItem>

            <MenuItem component="a" href="https://github.com/linkerd/linkerd2/issues/new/choose" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon>{githubIcon}</ListItemIcon>
              <ListItemText primary="File an Issue" />
            </MenuItem>
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
