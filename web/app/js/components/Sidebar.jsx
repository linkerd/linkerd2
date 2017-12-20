import { Link } from 'react-router-dom';
import logo from './../../img/reversed_logo.png';
import { Menu } from 'antd';
import React from 'react';
import Version from './Version.jsx';
import './../../css/sidebar.css';

export default class Sidebar extends React.Component {
  render() {
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");

    return (
      <div className="sidebar">
        <div className="list-container">
          <div className="sidebar-headers">
            <Link to={`${this.props.pathPrefix}/servicemesh`}><img src={logo} /></Link>
          </div>
          <Menu className="sidebar-menu" theme="dark" selectedKeys={[normalizedPath]}>
            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <Link to={`${this.props.pathPrefix}/servicemesh`}>Service mesh</Link>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/deployments">
              <Link to={`${this.props.pathPrefix}/deployments`}>Deployments</Link>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/routes">
              <Link to={`${this.props.pathPrefix}/routes`}>Routes</Link>
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/docs">
              <Link to="https://conduit.io/docs/" target="_blank">Documentation</Link>
            </Menu.Item>
          </Menu>
          <Version
            releaseVersion={this.props.releaseVersion}
            uuid={this.props.uuid} />
        </div>
      </div>
    );
  }

}
