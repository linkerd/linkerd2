import { ApiHelpers } from './util/ApiHelpers.jsx';
import { Layout } from 'antd';
import { Link } from 'react-router-dom';
import logo from './../../img/logo_only.png';
import React from 'react';
import SocialLinks from './SocialLinks.jsx';
import Version from './Version.jsx';
import wordLogo from './../../img/reversed_logo.png';
import { Icon, Menu } from 'antd';
import './../../css/sidebar.css';

export default class Sidebar extends React.Component {
  constructor(props) {
    super(props);
    this.api= this.props.api;
    this.toggleCollapse = this.toggleCollapse.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.handleApiError = this.handleApiError.bind(this);

    this.state = {
      initialCollapse: true,
      collapsed: true,
      error: null,
      latest: null,
      isLatest: true,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.loadFromServer();
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let versionUrl = `https://versioncheck.conduit.io/version.json?version=${this.props.releaseVersion}?uuid=${this.props.uuid}`;
    let versionFetch = ApiHelpers("").fetch(versionUrl);
    // expose serverPromise for testing
    this.serverPromise = versionFetch.promise
      .then(resp => {
        this.setState({
          latestVersion: resp.version,
          isLatest: resp.version === this.props.releaseVersion,
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
    let ConduitLink = this.api.ConduitLink;

    return (
      <Layout.Sider
        width="260px"
        breakpoint="lg"
        collapsed={this.state.collapsed}
        collapsible={true}
        onCollapse={this.toggleCollapse}>

        <div className="sidebar">

          <div className={`sidebar-menu-header ${this.state.collapsed ? "collapsed" : ""}`}>
            <ConduitLink to="/servicemesh"><img src={this.state.collapsed ? logo : wordLogo} /></ConduitLink>
          </div>

          <Menu
            className="sidebar-menu"
            theme="dark"
            selectedKeys={[normalizedPath]}>

            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <Icon><ConduitLink to="/servicemesh"><Icon type="home" /></ConduitLink></Icon>
              <span><ConduitLink to="/servicemesh">Service mesh</ConduitLink></span>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/namespaces">
              <Icon><ConduitLink to="/namespaces"><Icon>N</Icon></ConduitLink></Icon>
              <span><ConduitLink to="/namespaces">Namespaces</ConduitLink></span>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/deployments">
              <Icon><ConduitLink to="/deployments"><Icon>D</Icon></ConduitLink></Icon>
              <span><ConduitLink to="/deployments">Deployments</ConduitLink></span>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/replicationcontrollers">
              <Icon><ConduitLink to="/replicationcontrollers"><Icon>R</Icon></ConduitLink></Icon>
              <span><ConduitLink to="/replicationcontrollers">Replication Controllers</ConduitLink></span>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/pods">
              <Icon><ConduitLink to="/pods"><Icon>P</Icon></ConduitLink></Icon>
              <span><ConduitLink to="/pods">Pods</ConduitLink></span>
            </Menu.Item>

            <Menu.Item className="sidebar-menu-item" key="/docs">
              <Icon><Link to="https://conduit.io/docs/" target="_blank"><Icon type="solution" /></Link></Icon>
              <span><Link to="https://conduit.io/docs/" target="_blank">Documentation</Link></span>
            </Menu.Item>

            { this.state.isLatest ? null :
              <Menu.Item className="sidebar-menu-item" key="/update">
                <Icon><Link to="https://versioncheck.conduit.io/update" target="_blank"><Icon type="exclamation-circle-o" className="update" /></Link></Icon>
                <span><Link to="https://versioncheck.conduit.io/update" target="_blank">Update Conduit</Link></span>
              </Menu.Item>
            }
          </Menu>

          { this.state.collapsed ? null :
            <div className="sidebar-menu-footer">
              <SocialLinks />
              <Version
                isLatest={this.state.isLatest}
                latest={this.state.latestVersion}
                releaseVersion={this.props.releaseVersion}
                error={this.state.error}
                uuid={this.props.uuid} />
            </div>
          }
        </div>
      </Layout.Sider>
    );
  }
}
