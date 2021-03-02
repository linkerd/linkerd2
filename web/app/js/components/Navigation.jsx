import { cronJobIcon, daemonsetIcon, deploymentIcon, githubIcon, jobIcon, linkerdWordLogo, namespaceIcon, podIcon, replicaSetIcon, slackIcon, statefulSetIcon } from './util/SvgWrappers.jsx';
import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import AppBar from '@material-ui/core/AppBar';
import Autocomplete from '@material-ui/lab/Autocomplete';
import Badge from '@material-ui/core/Badge';
import BreadcrumbHeader from './BreadcrumbHeader.jsx';
import Divider from '@material-ui/core/Divider';
import Drawer from '@material-ui/core/Drawer';
import EmailIcon from '@material-ui/icons/Email';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Hidden from '@material-ui/core/Hidden';
import IconButton from '@material-ui/core/IconButton';
import LibraryBooksIcon from '@material-ui/icons/LibraryBooks';
import { Link } from 'react-router-dom';
import ListItemIcon from '@material-ui/core/ListItemIcon';
import ListItemText from '@material-ui/core/ListItemText';
import MenuItem from '@material-ui/core/MenuItem';
import MenuList from '@material-ui/core/MenuList';
import NamespaceConfirmationModal from './NamespaceConfirmationModal.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import TextField from '@material-ui/core/TextField';
import Toolbar from '@material-ui/core/Toolbar';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import Version from './Version.jsx';
import _isEmpty from 'lodash/isEmpty';
import _maxBy from 'lodash/maxBy';
import { faBars } from '@fortawesome/free-solid-svg-icons/faBars';
import { faCloud } from '@fortawesome/free-solid-svg-icons/faCloud';
import { faDungeon } from '@fortawesome/free-solid-svg-icons/faDungeon';
import { faExternalLinkAlt } from '@fortawesome/free-solid-svg-icons/faExternalLinkAlt';
import { faFilter } from '@fortawesome/free-solid-svg-icons/faFilter';
import { faMicroscope } from '@fortawesome/free-solid-svg-icons/faMicroscope';
import { faRandom } from '@fortawesome/free-solid-svg-icons/faRandom';
import { faSmile } from '@fortawesome/free-regular-svg-icons/faSmile';
import { faStream } from '@fortawesome/free-solid-svg-icons/faStream';
import grey from '@material-ui/core/colors/grey';
import { processSingleResourceRollup } from './util/MetricUtils.jsx';
import { regexFilterString } from './util/Utils.js';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import yellow from '@material-ui/core/colors/yellow';

const jsonFeedUrl = 'https://linkerd.io/dashboard/index.json';
const multiclusterExtensionName = 'multicluster';
const localStorageKey = 'linkerd-updates-last-clicked';
const minBrowserWidth = 960;

const styles = theme => {
  const drawerWidth = theme.spacing(36);
  const navLogoWidth = theme.spacing(22.5);
  const contentPadding = theme.spacing(3);

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
      alignItems: 'center',
      position: 'permanent',
      color: 'white',
      transition: leaving,
    },
    bars: {
      color: 'white',
      position: 'fixed',
      left: theme.spacing(2.5),
    },
    breadcrumbs: {
      color: 'white',
      marginLeft: `${drawerWidth}px`,
    },
    drawer: {
      width: drawerWidth,
      transition: entering,
    },
    drawerPaper: {
      width: 'inherit',
    },
    toolbar: theme.mixins.toolbar,
    navToolbar: {
      display: 'flex',
      alignItems: 'center',
      padding: `0 0 0 ${theme.spacing(2)}px`,
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
    linkerdNavLogo: {
      margin: 'auto',
      width: `${navLogoWidth}px`,
      transition: enteringFn(['margin', 'opacity']),
    },
    linkerdMobileLogo: {
      width: `${navLogoWidth}px`,
    },
    namespaceChangeButton: {
      borderRadius: '5px',
      backgroundColor: grey[400],
      marginLeft: `${drawerWidth * 0.075}px`,
      marginRight: `${drawerWidth * 0.075}px`,
      marginTop: '11px',
      width: `${drawerWidth * 0.85}px`,
    },
    namespaceChangeButtonInputRoot: {
      backgroundColor: grey[300],
      boxShadow: 'rgba(0, 0, 0, 0.2) 0px 3px 1px -2px, rgba(0, 0, 0, 0.14) 0px 2px 2px 0px, rgba(0, 0, 0, 0.12) 0px 1px 5px 0px',
      padding: '4px 12px !important',
      border: 0,
      '&:hover': {
        borderColor: 'transparent',
      },
    },
    namespaceChangeButtonInput: {
      textAlign: 'center',
    },
    namespaceChangeButtonInputFocused: {
      textAlign: 'center',
    },
    namespaceChangeButtonPopupIndicator: {
      backgroundColor: 'transparent',
      '&:hover': {
        backgroundColor: 'transparent',
      },
    },
    navMenuItem: {
      paddingLeft: `${contentPadding}px`,
      paddingRight: `${contentPadding}px`,
    },
    shrinkIcon: {
      fontSize: '24px',
      paddingLeft: '3px',
      paddingRight: '3px',
    },
    shrinkCloudIcon: {
      fontSize: '18px',
      paddingLeft: '1px',
    },
    // color is consistent with Octopus Graph coloring
    externalLinkIcon: {
      color: grey[500],
    },
    sidebarHeading: {
      color: grey[500],
      outline: 'none',
      paddingTop: '9px',
      paddingBottom: '9px',
      marginLeft: `${drawerWidth * 0.09}px`,
    },
    badge: {
      backgroundColor: yellow[500],
    },
    inputBase: {
      boxSizing: 'border-box',
    },
  };
};

class NavigationBase extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.handleConfirmNamespaceChange = this.handleConfirmNamespaceChange.bind(this);
    this.handleCommunityClick = this.handleCommunityClick.bind(this);
    this.handleDialogCancel = this.handleDialogCancel.bind(this);
    this.handleFilterInputChange = this.handleFilterInputChange.bind(this);
    this.handleNamespaceMenuClick = this.handleNamespaceMenuClick.bind(this);
    this.updateWindowDimensions = this.updateWindowDimensions.bind(this);
    this.handleAutocompleteClick = this.handleAutocompleteClick.bind(this);

    this.state = this.getInitialState();
    this.loadFromServer = this.loadFromServer.bind(this);
  }

  getInitialState() {
    return {
      mobileSidebarOpen: false,
      newNamespace: '',
      formattedNamespaceFilter: '',
      hideUpdateBadge: true,
      latestVersion: '',
      isLatest: true,
      namespaces: [],
      pendingRequests: false,
      pollingInterval: 10000,
      loaded: false,
      error: null,
      showNamespaceChangeDialog: false,
      showGatewayLink: false,
    };
  }

  componentDidMount() {
    this.startServerPolling();
    this.fetchVersion();
    this.checkMulticlusterExtension();
    this.fetchLatestCommunityUpdate();
    this.updateWindowDimensions();
    window.addEventListener('resize', this.updateWindowDimensions);
  }

  componentDidUpdate(prevProps) {
    const { history, checkNamespaceMatch, isPageVisible } = this.props;
    if (history) {
      checkNamespaceMatch(history.location.pathname);
    }

    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  componentWillUnmount() {
    window.removeEventListener('resize', this.updateWindowDimensions);
    this.stopServerPolling();
  }

  startServerPolling() {
    const { pollingInterval } = this.state;
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({ pendingRequests: false });
  }

  // API returns namespaces for namespace select button. No metrics returned.
  loadFromServer() {
    const { pendingRequests } = this.state;

    if (pendingRequests) {
      return;
    }
    this.setState({ pendingRequests: true });

    const apiRequests = [
      this.api.fetchMetrics(this.api.urlsForResourceNoStats('namespace')),
    ];

    this.api.setCurrentRequests(apiRequests);

    Promise.all(this.api.getCurrentPromises())
      .then(([allNs]) => {
        // add "All Namespaces" to the options
        let namespaces = [{ name: '_all', key: 'ns-all' }];
        namespaces = namespaces.concat(processSingleResourceRollup(allNs));
        this.setState({
          namespaces,
          pendingRequests: false,
          error: null,
        });
      })
      .catch(this.handleApiError);
  }

  fetchVersion() {
    const { releaseVersion, uuid } = this.props;

    const versionUrl = `https://versioncheck.linkerd.io/version.json?version=${releaseVersion}&uuid=${uuid}&source=web`;
    this.versionPromise = fetch(versionUrl, { credentials: 'include' })
      .then(rsp => rsp.json())
      .then(versionRsp => {
        let latestVersion;
        const parts = releaseVersion.split('-', 2);
        if (parts.length === 2) {
          latestVersion = versionRsp[parts[0]];
        }
        this.setState({
          latestVersion,
          isLatest: latestVersion === releaseVersion,
        });
      }).catch(this.handleApiError);
  }

  fetchLatestCommunityUpdate() {
    this.communityUpdatesPromise = fetch(jsonFeedUrl)
      .then(rsp => rsp.json())
      .then(rsp => rsp.data.date)
      .then(rsp => {
        if (rsp.length > 0) {
          const lastClicked = localStorage[localStorageKey];
          if (!lastClicked) {
            this.setState({ hideUpdateBadge: false });
          } else {
            const lastClickedDateObject = new Date(lastClicked);
            const latestArticle = _maxBy(rsp, update => update.date);
            const latestArticleDateObject = new Date(latestArticle);
            if (latestArticleDateObject > lastClickedDateObject) {
              this.setState({ hideUpdateBadge: false });
            }
          }
        }
      })
      .catch(this.handleApiError);
  }

  checkMulticlusterExtension() {
    this.api.setCurrentRequests([this.api.fetchExtension(multiclusterExtensionName)]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([extension]) => {
        this.setState({ showGatewayLink: !_isEmpty(extension) });
      })
      .catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      error: e,
    });
  }

  handleCommunityClick = () => {
    const lastClicked = new Date();
    localStorage.setItem(localStorageKey, lastClicked);
    this.setState({ hideUpdateBadge: true });
  }

  handleDialogCancel = () => {
    this.setState({ showNamespaceChangeDialog: false });
  }

  handleDrawerClick = () => {
    const { mobileSidebarOpen } = this.state;
    if (!mobileSidebarOpen) {
      this.setState({ mobileSidebarOpen: true });
    } else {
      this.setState({ mobileSidebarOpen: false });
      window.setTimeout(() => {
        const linkerdHash = document.querySelector('.linkerd-word-logo #linkerd-hash');
        linkerdHash.style.display = 'none';
        window.setTimeout(() => {
          linkerdHash.style.display = '';
        }, 15);
      }, 300);
    }
  };

  handleConfirmNamespaceChange = () => {
    const { newNamespace } = this.state;
    const { updateNamespaceInContext, history } = this.props;
    this.setState({ showNamespaceChangeDialog: false });
    updateNamespaceInContext(newNamespace);
    history.push(`/namespaces/${newNamespace}`);
  }

  handleFilterInputChange = event => {
    this.setState({
      formattedNamespaceFilter: regexFilterString(event.target.value) });
  }

  handleAutocompleteClick = event => {
    // This is necessary for the mobile sidebar, otherwise the sidebar
    // would close upon click of the namespace change input.
    event.stopPropagation();
  }

  handleNamespaceChange = (event, values) => {
    const { history, updateNamespaceInContext, selectedNamespace } = this.props;

    // event.stopPropagation();
    const namespace = values.name;
    if (namespace === selectedNamespace) {
      return;
    }
    let path = history.location.pathname;
    const pathParts = path.split('/');
    if (pathParts.length === 3 || pathParts.length === 4) {
      // path is /namespaces/someNamespace/resourceType
      //      or /namespaces/someNamespace
      path = path.replace(selectedNamespace, namespace);
      history.push(path);
      updateNamespaceInContext(namespace);
    } else if (pathParts.length === 5) {
      // path is /namespace/someNamespace/resourceType/someResource
      this.setState({
        showNamespaceChangeDialog: true,
        newNamespace: namespace,
      });
    } else {
      // update the selectedNamespace in context with no path changes
      updateNamespaceInContext(namespace);
    }
  }

  handleNamespaceMenuClick = event => {
    // ensure that mobile drawer will not close on click
    event.stopPropagation();
    this.setState({ formattedNamespaceFilter: '' });
  }

  menuItem(path, title, icon, onClick) {
    const { classes, location, pathPrefix } = this.props;
    const normalizedPath = location.pathname.replace(pathPrefix, '');

    return (
      <MenuItem
        component={Link}
        onClick={onClick}
        to={this.api.prefixLink(path)}
        className={classes.navMenuItem}
        selected={path === normalizedPath}>
        <ListItemIcon>{icon}</ListItemIcon>
        <ListItemText primary={title} />
      </MenuItem>
    );
  }

  updateWindowDimensions() {
    const browserWidth = window.innerWidth;
    if (browserWidth > minBrowserWidth) {
      this.setState({ mobileSidebarOpen: false });
    }
  }

  render() {
    const { api, classes, selectedNamespace, ChildComponent, uuid, releaseVersion, ...otherProps } = this.props;
    const { namespaces, formattedNamespaceFilter, hideUpdateBadge, isLatest, latestVersion,
      showNamespaceChangeDialog, newNamespace, mobileSidebarOpen, error, showGatewayLink } = this.state;
    const filteredNamespaces = namespaces.filter(ns => {
      return ns.name.match(formattedNamespaceFilter);
    });
    let formattedNamespaceName = selectedNamespace;
    if (formattedNamespaceName === '_all') {
      formattedNamespaceName = 'All Namespaces';
    }

    const drawer = (
      <div>
        { !mobileSidebarOpen &&
        <div className={classes.navToolbar}>
          <div className={classes.linkerdNavLogo}>
            <Link to="/namespaces">{linkerdWordLogo}</Link>
          </div>
        </div>
        }
        <Divider />
        <MenuList>
          <Typography variant="button" component="div" className={classes.sidebarHeading}>
            <Trans>sidebarHeadingCluster</Trans>
          </Typography>
          { this.menuItem('/namespaces', <Trans>menuItemNamespaces</Trans>, namespaceIcon) }

          { this.menuItem('/controlplane', <Trans>menuItemControlPlane</Trans>,
            <FontAwesomeIcon icon={faCloud} className={classes.shrinkCloudIcon} />) }

          { showGatewayLink && this.menuItem('/gateways', <Trans>menuItemGateway</Trans>,
            <FontAwesomeIcon icon={faDungeon} className={classes.shrinkIcon} />) }

        </MenuList>

        <Divider />

        <Autocomplete
          id="namespace-autocomplete"
          onClick={this.handleAutocompleteClick}
          disableClearable
          value={{ name: formattedNamespaceName.toUpperCase() }}
          options={filteredNamespaces}
          autoSelect
          getOptionSelected={option => option.name === selectedNamespace}
          getOptionLabel={option => { if (option.name !== '_all') { return option.name; } else { return 'All Namespaces'; } }}
          onChange={this.handleNamespaceChange}
          size="small"
          classes={{
            root: classes.namespaceChangeButton,
            inputRoot: classes.namespaceChangeButtonInputRoot,
            input: classes.namespaceChangeButtonInput,
            popupIndicator: classes.namespaceChangeButtonPopupIndicator,
          }}
          className={classes.namespaceChangeButton}
          renderInput={params => (
            <TextField
              {...params}
              key={params.name}
              variant="outlined"
              fullWidth />
          )} />

        <NamespaceConfirmationModal
          open={showNamespaceChangeDialog}
          selectedNamespace={selectedNamespace}
          newNamespace={newNamespace}
          handleDialogCancel={this.handleDialogCancel}
          handleConfirmNamespaceChange={this.handleConfirmNamespaceChange} />

        <MenuList>
          <Typography variant="button" component="div" className={classes.sidebarHeading}>
            <Trans>sidebarHeadingWorkloads</Trans>
          </Typography>

          { this.menuItem(`/namespaces/${selectedNamespace}/cronjobs`, <Trans>menuItemCronJobs</Trans>, cronJobIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/daemonsets`, <Trans>menuItemDaemonSets</Trans>, daemonsetIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/deployments`, <Trans>menuItemDeployments</Trans>, deploymentIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/jobs`, <Trans>menuItemJobs</Trans>, jobIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/pods`, <Trans>menuItemPods</Trans>, podIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/replicasets`, <Trans>menuItemReplicaSets</Trans>, replicaSetIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/replicationcontrollers`, <Trans>menuItemReplicationControllers</Trans>, replicaSetIcon) }

          { this.menuItem(`/namespaces/${selectedNamespace}/statefulsets`, <Trans>menuItemStatefulSets</Trans>, statefulSetIcon) }
        </MenuList>

        <MenuList>
          <Typography variant="button" component="div" className={classes.sidebarHeading}>
            <Trans>sidebarHeadingConfiguration</Trans>
          </Typography>

          { this.menuItem(`/namespaces/${selectedNamespace}/trafficsplits`, <Trans>menuItemTrafficSplits</Trans>, <FontAwesomeIcon icon={faFilter} className={classes.shrinkIcon} />) }

        </MenuList>
        <Divider />
        <MenuList>
          <Typography variant="button" component="div" className={classes.sidebarHeading}>
            <Trans>sidebarHeadingTools</Trans>
          </Typography>

          { this.menuItem('/tap', <Trans>menuItemTap</Trans>, <FontAwesomeIcon icon={faMicroscope} className={classes.shrinkIcon} />) }
          { this.menuItem('/top', <Trans>menuItemTop</Trans>, <FontAwesomeIcon icon={faStream} className={classes.shrinkIcon} />) }
          { this.menuItem('/routes', <Trans>menuItemRoutes</Trans>, <FontAwesomeIcon icon={faRandom} className={classes.shrinkIcon} />) }

        </MenuList>
        <Divider />
        <MenuList>
          { this.menuItem('/community', <Trans>menuItemCommunity</Trans>,
            <Badge
              classes={{ badge: classes.badge }}
              invisible={hideUpdateBadge}
              badgeContent="1">
              <FontAwesomeIcon icon={faSmile} className={classes.shrinkIcon} />
            </Badge>, this.handleCommunityClick) }

          <MenuItem component="a" href="https://linkerd.io/2/overview/" target="_blank" className={classes.navMenuItem}>
            <ListItemIcon><LibraryBooksIcon className={classes.shrinkIcon} /></ListItemIcon>
            <ListItemText primary={<Trans>menuItemDocumentation</Trans>} />
            <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
          </MenuItem>

          <MenuItem component="a" href="https://github.com/linkerd/linkerd2/issues/new/choose" target="_blank" className={classes.navMenuItem}>
            <ListItemIcon>{githubIcon}</ListItemIcon>
            <ListItemText primary={<Trans>menuItemGitHub</Trans>} />
            <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
          </MenuItem>

          <MenuItem component="a" href="https://lists.cncf.io/g/cncf-linkerd-users" target="_blank" className={classes.navMenuItem}>
            <ListItemIcon><EmailIcon className={classes.shrinkIcon} /></ListItemIcon>
            <ListItemText primary={<Trans>menuItemMailingList</Trans>} />
            <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
          </MenuItem>

          <MenuItem component="a" href="https://slack.linkerd.io" target="_blank" className={classes.navMenuItem}>
            <ListItemIcon>{slackIcon}</ListItemIcon>
            <ListItemText primary={<Trans>menuItemSlack</Trans>} />
            <FontAwesomeIcon icon={faExternalLinkAlt} className={classes.externalLinkIcon} size="xs" />
          </MenuItem>

          <Version
            isLatest={isLatest}
            latestVersion={latestVersion}
            releaseVersion={releaseVersion}
            error={error}
            uuid={uuid} />

        </MenuList>

      </div>
    );

    return (
      <div className={classes.root}>

        <Hidden smDown>
          <Drawer
            className={classes.drawer}
            classes={{ paper: classes.drawerPaper }}
            variant="permanent">
            {drawer}
          </Drawer>
          <AppBar>
            <Toolbar>
              <Typography variant="h6" color="inherit" className={classes.breadcrumbs} noWrap>
                <BreadcrumbHeader {...this.props} />
              </Typography>
            </Toolbar>
          </AppBar>
        </Hidden>

        <Hidden mdUp>
          <AppBar className={classes.appBar}>
            <Toolbar>
              <div className={classes.linkerdMobileLogo}>
                {linkerdWordLogo}
              </div>
              { !mobileSidebarOpen && // mobile view but no sidebar
              <React.Fragment>
                <IconButton onClick={this.handleDrawerClick} className={classes.bars}>
                  <FontAwesomeIcon icon={faBars} />
                </IconButton>
              </React.Fragment>
              }
            </Toolbar>
          </AppBar>
          <Drawer
            className={classes.drawer}
            classes={{ paper: classes.drawerPaper }}
            variant="temporary"
            onClick={this.handleDrawerClick}
            onClose={this.handleDrawerClick}
            open={mobileSidebarOpen}>
            {drawer}
          </Drawer>
        </Hidden>

        <main className={classes.content}>
          <div className={classes.toolbar} />
          <div>
            <ChildComponent {...otherProps} />
          </div>
        </main>
      </div>
    );
  }
}

NavigationBase.propTypes = {
  api: PropTypes.shape({}).isRequired,
  checkNamespaceMatch: PropTypes.func.isRequired,
  ChildComponent: PropTypes.oneOfType([
    PropTypes.func,
    PropTypes.object,
  ]).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  history: ReactRouterPropTypes.history.isRequired,
  location: ReactRouterPropTypes.location.isRequired,
  pathPrefix: PropTypes.string.isRequired,
  releaseVersion: PropTypes.string.isRequired,
  selectedNamespace: PropTypes.string.isRequired,
  theme: PropTypes.shape({}).isRequired,
  updateNamespaceInContext: PropTypes.func.isRequired,
  uuid: PropTypes.string.isRequired,
};

export default withPageVisibility(withContext(withStyles(styles, { withTheme: true })(NavigationBase)));
