import { daemonsetIcon, deploymentIcon, githubIcon, jobIcon, linkerdWordLogo, namespaceIcon, podIcon, replicaSetIcon, slackIcon, statefulSetIcon } from './util/SvgWrappers.jsx';
import { processMultiResourceRollup, processSingleResourceRollup } from './util/MetricUtils.jsx';
import AppBar from '@material-ui/core/AppBar';
import ArrowDropDownIcon from '@material-ui/icons/ArrowDropDown';
import Badge from '@material-ui/core/Badge';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import Button from '@material-ui/core/Button';
import ChevronLeftIcon from '@material-ui/icons/ChevronLeft';
import CloudIcon from '@material-ui/icons/Cloud';
import Divider from '@material-ui/core/Divider';
import Drawer from '@material-ui/core/Drawer';
import EmailIcon from '@material-ui/icons/Email';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import HomeIcon from '@material-ui/icons/Home';
import IconButton from '@material-ui/core/IconButton';
import LibraryBooksIcon from '@material-ui/icons/LibraryBooks';
import { Link } from 'react-router-dom';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import Menu from '@material-ui/core/Menu';
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
import _find from 'lodash/find';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import _maxBy from 'lodash/maxBy';
import classNames from 'classnames';
import { faExternalLinkAlt } from '@fortawesome/free-solid-svg-icons/faExternalLinkAlt';
import { faFilter } from '@fortawesome/free-solid-svg-icons/faFilter';
import { faMicroscope } from '@fortawesome/free-solid-svg-icons/faMicroscope';
import { faRandom } from '@fortawesome/free-solid-svg-icons/faRandom';
import { faStream } from '@fortawesome/free-solid-svg-icons/faStream';
import grey from '@material-ui/core/colors/grey';
import teal from '@material-ui/core/colors/teal';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import yellow from '@material-ui/core/colors/yellow';

const jsonFeedUrl = "https://linkerd.io/dashboard/index.json";
const localStorageKey = "linkerd-updates-last-clicked";
const minBrowserWidth = 960;

const styles = theme => {
  const drawerWidth = theme.spacing.unit * 36;
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
    namespaceChangeButton: {
      marginLeft: `${drawerWidth * .075}px`,
      width: `${drawerWidth * .8}px`,
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
    // color is consistent with Octopus Graph coloring
    externalLinkIcon: {
      color: grey[500],
    },
    sidebarHeading: {
      color: grey[500],
      marginLeft: `${drawerWidth * .03}px`,
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
    this.handleNamespaceMenuClick = this.handleNamespaceMenuClick.bind(this);
    this.updateWindowDimensions = this.updateWindowDimensions.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      drawerOpen: true,
      namespaceMenuOpen: false,
      hideUpdateBadge: true,
      latestVersion: '',
      isLatest: true,
      namespaceFilter: "all",
      namespaces: []
    };
  }

  componentDidMount() {
    this.loadFromServer();
    this.fetchVersion();
    this.fetchLatestCommunityUpdate();
    this.updateWindowDimensions();
    window.addEventListener("resize", this.updateWindowDimensions);
  }

  componentWillUnmount() {
    window.removeEventListener("resize", this.updateWindowDimensions);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    // TODO: make this one request
    let apiRequests = [
      this.api.fetchMetrics(this.api.urlsForResourceNoStats("namespace"))
    ];
    if (!_isEmpty(this.state.nsMetrics)) {
      apiRequests = apiRequests.concat([
        this.api.fetchMetrics(this.api.urlsForResource("all", this.state.nsMetrics, true))
      ]);
    }
    this.api.setCurrentRequests(apiRequests);

    Promise.all(this.api.getCurrentPromises())
      .then(([allNs, metricsForNs]) => {
        let namespaces = processSingleResourceRollup(allNs);
        this.setState({
          namespaces,
          //nsMetrics: this.state.nsMetrics || defaultOpenNs.name,
          pendingRequests: false,
          loaded: true,
          error: null
        });
      })
      .catch(this.handleApiError);
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

  changeNs = newNs => {
    const updateContext = this.props.changeNs;
    let newPath = this.props.history.location.pathname;
    newPath = newPath.replace(this.props.nsMetrics, newNs);
    this.props.history.push(newPath);
    updateContext(newNs);
    this.setState(state => ({ namespaceMenuOpen: false }));
  }

  handleNamespaceMenuClick = () => {
    this.setState(state => ({ namespaceMenuOpen: !state.namespaceMenuOpen }));
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
    const { api, classes, nsMetrics, ChildComponent, ...otherProps } = this.props;
    const namespaces = this.state.namespaces;

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
              <Link to="/namespaces">{linkerdWordLogo}</Link>
            </div>
            <IconButton className="drawer-toggle-btn" onClick={this.handleDrawerClick}>
              {this.state.drawerOpen ? <ChevronLeftIcon /> : <MenuIcon />}
            </IconButton>
          </div>

          <Divider />
          <MenuList>
            <MenuItem>
              <Typography variant="button" className={classes.sidebarHeading}>
                Cluster
              </Typography>
            </MenuItem>
            <MenuItem
              component={Link}
              to="/namespaces"
              key="namespaces"
              className={classes.navMenuItem}>
              <ListItemIcon>{namespaceIcon}</ListItemIcon>
              <ListItemText primary="Namespaces" />
            </MenuItem>
            { this.menuItem("/controlplane", "Control Plane", <CloudIcon className={classes.shrinkIcon} />) }
          </MenuList>
          <Divider />
          <MenuList>
            <Button
              variant="contained"
              className={classes.namespaceChangeButton}
              size="large"
              onClick={this.handleNamespaceMenuClick}>
              {nsMetrics}
              <ArrowDropDownIcon />
            </Button>
            <Menu
              id="simple-menu"
              open={this.state.namespaceMenuOpen}
              keepMounted
              onClose={this.handleNamespaceMenuClick}>
              <MenuItem
                value="all"
                onClick={() => this.changeNs("all")}
                key="all-namespaces">All Namespaces
              </MenuItem>
              <Divider />
              {namespaces.map(ns => (
                <MenuItem
                  onClick={() => this.changeNs(ns.name)}
                  key={ns.name}>{ns.name}
                </MenuItem>
              ))}

            </Menu>
          </MenuList>


          <MenuList>
            <MenuItem>
              <Typography variant="button" className={classes.sidebarHeading}>
                Workloads
              </Typography>
            </MenuItem>

            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/daemonsets`}
              key="workload-daemonsets"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/daemonsets` === window.location.pathname}>
              <ListItemIcon>{daemonsetIcon}</ListItemIcon>
              <ListItemText primary="Daemon Sets" />
            </MenuItem>

            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/deployments`}
              key="workload-deployments"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/deployments` === window.location.pathname}>
              <ListItemIcon>{deploymentIcon}</ListItemIcon>
              <ListItemText primary="Deployments" />
            </MenuItem>

            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/jobs`}
              key="workload-jobs"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/jobs` === window.location.pathname}>
              <ListItemIcon>{jobIcon}</ListItemIcon>
              <ListItemText primary="Jobs" />
            </MenuItem>

            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/pods`}
              key="workload-pods"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/pods` === window.location.pathname}>
              <ListItemIcon>{podIcon}</ListItemIcon>
              <ListItemText primary="Pods" />
            </MenuItem>


            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/replicationcontrollers`}
              key="workload-replicationcontrollers"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/replicationcontrollers` === window.location.pathname}>
              <ListItemIcon>{replicaSetIcon}</ListItemIcon>
              <ListItemText primary="Replication Controllers" />
            </MenuItem>

            <MenuItem
              component={Link}
              to={`/namespaces/${nsMetrics}/statefulsets`}
              key="workload-statefulsets"
              className={classes.navMenuItem}
              selected={`/namespaces/${nsMetrics}/statefulsets` === window.location.pathname}>
              <ListItemIcon>{statefulSetIcon}</ListItemIcon>
              <ListItemText primary="Stateful Sets" />
            </MenuItem>
          </MenuList>


          <MenuList>
            <MenuItem>
              <Typography variant="button" className={classes.sidebarHeading}>
                Configuration
              </Typography>
            </MenuItem>

            { this.menuItem(`/namespaces/${nsMetrics}/trafficsplits`, "Traffic Splits", <FontAwesomeIcon icon={faFilter} className={classes.shrinkIcon} />) }

          </MenuList>
          <Divider />
          <MenuList >
            <MenuItem>
              <Typography variant="button" className={classes.sidebarHeading}>
                Tools
              </Typography>
            </MenuItem>

            { this.menuItem("/tap", "Tap", <FontAwesomeIcon icon={faMicroscope} className={classes.shrinkIcon} />) }
            { this.menuItem("/top", "Top", <FontAwesomeIcon icon={faStream} className={classes.shrinkIcon} />) }
            { this.menuItem("/routes", "Routes", <FontAwesomeIcon icon={faRandom} className={classes.shrinkIcon} />) }
          </MenuList>
          <Divider />
          <MenuList>
            { this.menuItem("/community", "Community",
              <Badge
                classes={{ badge: classes.badge }}
                invisible={this.state.hideUpdateBadge}
                badgeContent="1">
                <SentimentVerySatisfiedIcon className={classes.shrinkIcon} />
              </Badge>, this.handleCommunityClick
              ) }

            <MenuItem component="a" href="https://linkerd.io/2/overview/" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon><LibraryBooksIcon className={classes.shrinkIcon} /></ListItemIcon>
              <ListItemText primary="Documentation" />
              <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
            </MenuItem>

            <MenuItem component="a" href="https://github.com/linkerd/linkerd2/issues/new/choose" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon>{githubIcon}</ListItemIcon>
              <ListItemText primary="GitHub" />
              <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
            </MenuItem>

            <MenuItem component="a" href="https://lists.cncf.io/g/cncf-linkerd-users" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon><EmailIcon className={classes.shrinkIcon} /></ListItemIcon>
              <ListItemText primary="Mailing List" />
              <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
            </MenuItem>


            <MenuItem component="a" href="https://slack.linkerd.io" target="_blank" className={classes.navMenuItem}>
              <ListItemIcon>{slackIcon}</ListItemIcon>
              <ListItemText primary="Slack" />
              <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
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
