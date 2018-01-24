import _ from 'lodash';
import { getPodsByDeployment } from './util/MetricUtils.js';
import logo from './../../img/reversed_logo.png';
import React from 'react';
import Version from './Version.jsx';
import { AutoComplete, Menu } from 'antd';
import './../../css/sidebar.css';

const searchBarWidth = 240;

export default class Sidebar extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.filterDeployments = this.filterDeployments.bind(this);
    this.onAutocompleteSelect = this.onAutocompleteSelect.bind(this);
    this.loadFromServer();

    this.state = {
      autocompleteValue: '',
      deployments: [],
      filteredDeployments: []
    };
  }

  loadFromServer() {
    this.api.fetchPods().then(r => {
      let deploys =  _.map(getPodsByDeployment(r.pods), 'name');

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
    let pathToDeploymentPage = `${this.props.pathPrefix}/deployment?deploy=${deployment}`;
    this.props.history.push(pathToDeploymentPage);
    this.setState({
      autocompleteValue: '',
      filteredDeployments: this.state.deployments
    });
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
    let ConduitLink = this.api.ConduitLink;
    return (
      <div className="sidebar">
        <div className="list-container">
          <div className="sidebar-headers">
            <ConduitLink to="/servicemesh" name={<img src={logo} />} />
          </div>

          <AutoComplete
            className="conduit-autocomplete"
            value={this.state.autocompleteValue}
            dataSource={this.state.filteredDeployments}
            style={{ width: searchBarWidth }}
            onSelect={this.onAutocompleteSelect}
            onSearch={this.filterDeployments}
            placeholder="Search by deployment" />

          <Menu className="sidebar-menu" theme="dark" selectedKeys={[normalizedPath]}>
            <Menu.Item className="sidebar-menu-item" key="/servicemesh">
              <ConduitLink to="/servicemesh" name="Service mesh" />
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/deployments">
              <ConduitLink to="/deployments" name="Deployments" />
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/routes">
              <ConduitLink to="/routes" name="Routes" />
            </Menu.Item>
            <Menu.Item className="sidebar-menu-item" key="/docs">
              <ConduitLink to="https://conduit.io/docs/" absolute="true" target="_blank" name="Documentation" />
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
