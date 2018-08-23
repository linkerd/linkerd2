import _ from 'lodash';
import { friendlyTitle } from './util/Utils.js';
import { Link } from 'react-router-dom';
import PropTypes from 'prop-types';
import React from 'react';
import ReactRouterPropTypes from 'react-router-prop-types';
import Version from './Version.jsx';
import { withContext } from './util/AppContext.jsx';
import { Badge, Form, Icon, Layout, Menu, Select } from 'antd';
import {
  excludeResourcesFromRollup,
  getSuccessRateClassification,
  processMultiResourceRollup,
  processSingleResourceRollup
} from './util/MetricUtils.js';
import {linkerdLogoOnly, linkerdWordLogo} from './util/SvgWrappers.jsx';
import './../../css/sidebar.css';
import 'whatwg-fetch';

const classificationLabels = {
  good: "success",
  neutral: "warning",
  bad: "error",
  default: "default"
};

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
    this.api = this.props.api;
    this.toggleCollapse = this.toggleCollapse.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);
    this.handleNamespaceSelector = this.handleNamespaceSelector.bind(this);

    this.state = this.getInitialState();
  }

  getInitialState() {
    return {
      pollingInterval: 12000,
      initialCollapse: false,
      collapsed: true,
      error: null,
      latestVersion: '',
      isLatest: true,
      pendingRequests: false,
      namespaceFilter: "all"
    };
  }

  componentDidMount() {
    this.fetchVersion();
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

  fetchVersion() {
    let versionUrl = `https://versioncheck.linkerd.io/version.json?version=${this.props.releaseVersion}&uuid=${this.props.uuid}`;
    fetch(versionUrl, { credentials: 'include' })
      .then(rsp => rsp.json())
      .then(versionRsp =>
        this.setState({
          latestVersion: versionRsp.version,
          isLatest: versionRsp.version === this.props.releaseVersion
        })
      ).catch(this.handleApiError);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([
      this.api.fetchMetrics(this.api.urlsForResource("all")),
      this.api.fetchMetrics(this.api.urlsForResource("namespace"))
    ]);

    // expose serverPromise for testing
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([allRsp, nsRsp]) => {
        let allResourceGroups = processMultiResourceRollup(allRsp);
        let finalResourceGroups = excludeResourcesFromRollup(allResourceGroups, ["authority", "service"]);
        let nsStats = processSingleResourceRollup(nsRsp);
        let namespaces = _(nsStats).map('name').sortBy().value();

        this.setState({
          finalResourceGroups,
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

  handleNamespaceSelector(value) {
    this.setState({
      namespaceFilter: value
    });
  }

  filterResourcesByNamespace(resources, namespace) {
    let resourceFilter = namespace === "all" ? r => r.added :
      r => r.namespace === namespace && r.added;

    return _.mapValues(resources, o => _.filter(o, resourceFilter));
  }

  render() {
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");
    let PrefixedLink = this.api.PrefixedLink;
    let namespaces = [
      {value: "all", name: "All Namespaces"}
    ].concat(_.map(this.state.namespaces, ns => {
      return {value: ns, name: ns};
    }));
    let sidebarComponents = this.filterResourcesByNamespace(this.state.finalResourceGroups, this.state.namespaceFilter);

    return (
      <Layout.Sider
        width="260px"
        breakpoint="lg"
        collapsed={this.state.collapsed}
        collapsible={true}
        onCollapse={this.toggleCollapse}>
        <div className="sidebar">

          <div className={`sidebar-menu-header ${this.state.collapsed ? "collapsed" : ""}`}>
            <PrefixedLink to="/overview">
              {this.state.collapsed ? linkerdLogoOnly : linkerdWordLogo}
            </PrefixedLink>
          </div>

          <Menu
            className="sidebar-menu"
            theme="dark"
            selectedKeys={[normalizedPath]}>

            <Menu.Item className="sidebar-menu-item" key="/overview">
              <PrefixedLink to="/overview">
                <Icon type="home" />
                <span>Overview</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/tap">
              <PrefixedLink to="/tap">
                <Icon type="filter" />
                <span>Tap</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/top">
              <PrefixedLink to="/top">
                <Icon type="caret-up" />
                <span>Top</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <PrefixedLink to="/servicemesh">
                <Icon type="cloud" />
                <span>Service mesh</span>
              </PrefixedLink>
            </Menu.Item>

            <Menu.SubMenu
              className="sidebar-menu-item"
              key="byresource"
              title={<span className="sidebar-title"><Icon type="bars" />{this.state.collapsed ? "" : "Resources"}</span>}>
              <Menu.Item><PrefixedLink to="/authorities">Authorities</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/deployments">Deployments</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/namespaces">Namespaces</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/pods">Pods</PrefixedLink></Menu.Item>
              <Menu.Item><PrefixedLink to="/replicationcontrollers">Replication Controllers</PrefixedLink></Menu.Item>
            </Menu.SubMenu>
            <Menu.Divider />
          </Menu>


          <Menu
            className="sidebar-menu"
            theme="dark"
            mode="inline"
            selectedKeys={[normalizedPath]}>
            {
              this.state.collapsed ? null : (
                <Menu.Item className="sidebar-menu-item" key="/namespace-selector">
                  <Form layout="inline">
                    <Form.Item>
                      <Select
                        defaultValue={this.state.namespaceFilter || "All Namespaces"}
                        dropdownMatchSelectWidth={true}
                        onChange={this.handleNamespaceSelector}>
                        {
                      _.map(namespaces, label => {
                        return (
                          <Select.Option key={label.value} value={label.value}>{label.name}</Select.Option>
                        );
                      })
                    }
                      </Select>
                    </Form.Item>
                  </Form>
                </Menu.Item>
              )
            }

            {
              this.state.collapsed ? null :
              _.map(_.keys(sidebarComponents).sort(), resourceName => {
                return (
                  <Menu.SubMenu
                    className="sidebar-menu-item"
                    key={resourceName}
                    title={friendlyTitle(resourceName).plural}
                    disabled={_.isEmpty(sidebarComponents[resourceName])}>
                    {
                      _.map(_.sortBy(sidebarComponents[resourceName], r => `${r.namespace}/${r.name}`), r => {
                        // only display resources that have been meshed
                        return (
                          <Menu.Item
                            className="sidebar-submenu-item"
                            title={`${r.namespace}/${r.name}`}
                            key={this.api.generateResourceURL(r)}>
                            <div>
                              <PrefixedLink
                                to={this.api.generateResourceURL(r)}>
                                {`${r.namespace}/${r.name}`}
                              </PrefixedLink>
                              <Badge status={getSuccessRateClassification(r.successRate, classificationLabels)} />
                            </div>
                          </Menu.Item>
                        );
                      })
                    }
                  </Menu.SubMenu>
                );
              })
            }

            <Menu.Item className="sidebar-menu-item" key="/docs">
              <Link to="https://linkerd.io/2/overview/" target="_blank">
                <Icon type="file-text" />
                <span>Documentation</span>
              </Link>
            </Menu.Item>

            {
              this.state.isLatest ? null : (
                <Menu.Item className="sidebar-menu-item" key="/update">
                  <Link to="https://versioncheck.linkerd.io/update" target="_blank">
                    <Icon type="exclamation-circle-o" className="update" />
                    <span>Update {this.props.productName}</span>
                  </Link>
                </Menu.Item>
              )
            }
          </Menu>

          {
            this.state.collapsed ? null : (
              <Version
                isLatest={this.state.isLatest}
                latestVersion={this.state.latestVersion}
                releaseVersion={this.props.releaseVersion}
                error={this.state.error}
                uuid={this.props.uuid} />
            )
          }
        </div>
      </Layout.Sider>
    );
  }
}

export default withContext(Sidebar);
