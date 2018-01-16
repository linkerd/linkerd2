import _ from 'lodash';
import { ApiHelpers } from './util/ApiHelpers.js';
import { Link } from 'react-router-dom';
import logo from './../../img/reversed_logo.png';
import React from 'react';
import Version from './Version.jsx';
import { AutoComplete, Menu } from 'antd';
import './../../css/sidebar.css';

const searchBarWidth = 240;

export default class Sidebar extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
    this.filterDeployments = this.filterDeployments.bind(this);
    this.onAutocompleteSelect = this.onAutocompleteSelect.bind(this);
    this.loadFromServer();

    this.state = {
      autocompleteValue: '',
      deployments: []
    };
  }

  loadFromServer() {
    this.api.fetchPods().then(r => {
      let deploys =  _(r.pods)
        .groupBy('deployment')
        .keys()
        .sort().value();
      this.setState({
        deployments: deploys,
        filteredDeployments: deploys
      });
    }).catch(this.handleApiError);
  }

  handleApiError(e) {
    console.warn(e.message);
  }

  onAutocompleteSelect(deployment) {
    let pathToDeploymentPage = `/deployment?deploy=${deployment}`;
    this.props.history.push(pathToDeploymentPage);
    this.setState({ autocompleteValue: '' });
  }

  filterDeployments(search) {
    this.setState({
      autocompleteValue: search,
      filteredDeployments: _.filter(this.state.deployments, d => {
        return d.indexOf(search) !== -1;
      })
    });
  }

  render() {
    let normalizedPath = this.props.location.pathname.replace(this.props.pathPrefix, "");

    return (
      <div className="sidebar">
        <div className="list-container">
          <div className="sidebar-headers">
            <Link to={`${this.props.pathPrefix}/servicemesh`}><img src={logo} /></Link>
          </div>

          <AutoComplete
            value={this.state.autocompleteValue}
            dataSource={this.state.filteredDeployments}
            style={{ width: searchBarWidth }}
            onSelect={this.onAutocompleteSelect}
            onSearch={this.filterDeployments}
            placeholder="Search by deployment" />

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
