import _ from 'lodash';
import ApiHelpers from './util/ApiHelpers.jsx';
import { Link } from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import SocialLinks from './SocialLinks.jsx';
import Version from './Version.jsx';
import { withContext } from './util/AppContext.jsx';
import { Icon, Layout, Menu } from 'antd';
import { linkerdLogoOnly, linkerdWordLogo } from './util/SvgWrappers.jsx';
import './../../css/sidebar.css';

class Sidebar extends React.Component {
  static defaultProps = {
    productName: 'controller'
  }

  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    location: ReactRouterPropTypes.location.isRequired,
    pathPrefix: PropTypes.string.isRequired,
    productName: PropTypes.string,
    releaseVersion: PropTypes.string.isRequired,
    uuid: PropTypes.string.isRequired,
  }

  constructor(props) {
    super(props);
    this.api= this.props.api;
    this.toggleCollapse = this.toggleCollapse.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      pollingInterval: 12000,
      maxNsItemsToShow: 8,
      initialCollapse: true,
      collapsed: true,
      error: null,
      latestVersion: '',
      isLatest: true,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentWillUnmount() {
    // the Sidebar never unmounts, but if something ever does, we should take care of it
    this.stopServerPolling();
  }

  startServerPolling() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }


  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let versionUrl = `https://versioncheck.linkerd.io/version.json?version=${this.props.releaseVersion}?uuid=${this.props.uuid}`;
    this.api.setCurrentRequests([
      ApiHelpers("").fetch(versionUrl),
      this.api.fetchMetrics(this.api.urlsForResource("namespace"))
    ]);

    // expose serverPromise for testing
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([versionRsp, nsRsp]) => {
        let nsStats = _.get(nsRsp, ["ok", "statTables", 0, "podGroup", "rows"], []);
        let namespaces = _(nsStats).map(r => r.resource.name).sortBy().value();

        this.setState({
          latestVersion: versionRsp.version,
          isLatest: versionRsp.version === this.props.releaseVersion,
          namespaces,
          pendingRequests: false,
        });
      }).catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      pendingRequests: false,
      error: e
    });
  }

  toggleCollapse() {
    if (this.state.initialCollapse) {
      // fix weird situation where toggleCollapsed is called on pageload,
      // causing the toggle states to be inconsistent. Don't toggle on the
      // very first call to toggleCollapse()
      this.setState({ initialCollapse: false});
    } else {
      this.setState({ collapsed: !this.state.collapsed });
    }
  }

  render() {
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");
    let PrefixedLink = this.api.PrefixedLink;
    let numHiddenNamespaces = _.size(this.state.namespaces) - this.state.maxNsItemsToShow;

    return (
      <Layout.Sider
        width="260px"
        breakpoint="lg"
        collapsed={this.state.collapsed}
        collapsible={true}
        onCollapse={this.toggleCollapse}>

        <div className="sidebar">

          <div className={`sidebar-menu-header ${this.state.collapsed ? "collapsed" : ""}`}>
            <PrefixedLink to="/servicemesh">
              {this.state.collapsed ? linkerdLogoOnly : linkerdWordLogo}
            </PrefixedLink>
          </div>

          <Menu
            className="sidebar-menu"
            theme="dark"
            selectedKeys={[normalizedPath]}>

            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <PrefixedLink to="/servicemesh">
                <Icon type="home" />
                <span>Service mesh</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/namespaces">
              <PrefixedLink to="/namespaces">
                <Icon type="dashboard" />
                <span>Namespaces</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/tap">
              <PrefixedLink to="/tap">
                <Icon type="filter" />
                <span>Tap</span>
              </PrefixedLink>
            </Menu.Item>

            {
              _.map(_.take(this.state.namespaces, this.state.maxNsItemsToShow), ns => {
                return (
                  <Menu.Item className="sidebar-submenu-item" key={`/namespaces/${ns}`}>
                    <PrefixedLink to={`/namespaces/${ns}`}>
                      <Icon>{this.state.collapsed ? _.take(ns, 2) : <span>&nbsp;&nbsp;</span> }</Icon>
                      <span>{ns} {this.state.collapsed ? "namespace" : ""}</span>
                    </PrefixedLink>
                  </Menu.Item>
                );
              })
            }

            { // if we're hiding some namespaces, show a count
              numHiddenNamespaces > 0 ?
                <Menu.Item className="sidebar-submenu-item" key="extra-items">
                  <PrefixedLink to="/namespaces">
                    <Icon>{this.state.collapsed ? <span>...</span> : <span>&nbsp;&nbsp;</span> }</Icon>
                    <span>{numHiddenNamespaces} more namespace{numHiddenNamespaces === 1 ? "" : "s"}</span>
                  </PrefixedLink>
                </Menu.Item>
                : null
            }

            <Menu.SubMenu
              className="sidebar-menu-item"
              key="byresource"
              title={<span className="sidebar-title"><Icon type="bars" />{this.state.collapsed ? "" : "Resources"}</span>}>
              <Menu.Item><PrefixedLink to="/authorities">Authorities</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/deployments">Deployments</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/pods">Pods</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/replicationcontrollers">Replication Controllers</PrefixedLink></Menu.Item>
            </Menu.SubMenu>

            <Menu.Item className="sidebar-menu-item" key="/docs">
              <Link to="https://linkerd.io/2/overview/" target="_blank">
                <Icon type="file-text" />
                <span>Documentation</span>
              </Link>
            </Menu.Item>

            { this.state.isLatest ? null : (
              <Menu.Item className="sidebar-menu-item" key="/update">
                <Link to="https://versioncheck.linkerd.io/update" target="_blank">
                  <Icon type="exclamation-circle-o" className="update" />
                  <span>Update {this.props.productName}</span>
                </Link>
              </Menu.Item>
            )}
          </Menu>

          { this.state.collapsed ? null : (
            <div className="sidebar-menu-footer">
              <SocialLinks />
              <Version
                isLatest={this.state.isLatest}
                latestVersion={this.state.latestVersion}
                releaseVersion={this.props.releaseVersion}
                error={this.state.error}
                uuid={this.props.uuid} />
            </div>
          )}
        </div>
      </Layout.Sider>
    );
  }
}

export default withContext(Sidebar);
