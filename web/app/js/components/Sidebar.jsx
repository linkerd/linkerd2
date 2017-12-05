import React from 'react';
import { Link } from 'react-router-dom';
import styles from './../../css/sidebar.css';
import { Input, Avatar, Menu } from 'antd';
import logo from './../../img/reversed_logo.png';


export default class Sidebar extends React.Component {
  render() {
    let normalizedPath = this.props.location.pathname.replace(/\/$/, "");

    return (
    <div className="sidebar">
      <div className="list-container">
        <div className="sidebar-headers">
          <Link to={`${this.props.pathPrefix}/servicemesh`}><img src={logo}/></Link>
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
            <Link to={`${this.props.pathPrefix}/docs`}>Docs</Link>
          </Menu.Item>
        </Menu>
          <div className="conduit-current-version">
            Running {this.props.releaseVersion}<br/>
            <iframe
              className="conduit-version-check"
              src={`https://versioncheck.conduit.io/version/${this.props.releaseVersion}?uuid=${this.props.uuid}`}
              sandbox="allow-top-navigation" />
          </div>
      </div>
    </div>
    )
  }

}
