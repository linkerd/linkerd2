import { Link } from 'react-router-dom';
import logo from './../../img/reversed_logo.png';
import { Menu } from 'antd';
import React from 'react';
import SocialLinks from './SocialLinks.jsx';
import Version from './Version.jsx';
import './../../css/sidebar.css';

export default class Sidebar extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
  }

  render() {
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");
    let ConduitLink = this.api.ConduitLink;

    return (
      <div className="sidebar">
        <div className="list-container">
          <div className="sidebar-headers">
            <ConduitLink to="/servicemesh"><img src={logo} /></ConduitLink>
          </div>

          <Menu className="sidebar-menu" theme="dark" selectedKeys={[normalizedPath]}>
            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <ConduitLink to="/servicemesh">Service mesh</ConduitLink>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/deployments">
              <ConduitLink to="/deployments">Deployments</ConduitLink>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/replicationcontrollers">
              <ConduitLink to="/replicationcontrollers">Replication Controllers</ConduitLink>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/pods">
              <ConduitLink to="/pods">Pods</ConduitLink>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/docs">
              <Link to="https://conduit.io/docs/" target="_blank">Documentation</Link>
            </Menu.Item>
          </Menu>

          {
            !this.props.collapsed ? (
              <React.Fragment>
                <SocialLinks />
                <Version
                  releaseVersion={this.props.releaseVersion}
                  uuid={this.props.uuid} />
              </React.Fragment>
            ) : null
          }
        </div>
      </div>
    );
  }

}
