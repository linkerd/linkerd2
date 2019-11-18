import { StringParam, withQueryParams } from 'use-query-params';
import { emptyTapQuery, tapQueryPropType, tapQueryProps } from './util/TapUtils.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TapQueryForm from './TapQueryForm.jsx';
import TopModule from './TopModule.jsx';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _merge from 'lodash/merge';
import _reduce from 'lodash/reduce';
import { withContext } from './util/AppContext.jsx';

let topQueryConfig = {};
for (let value in tapQueryProps) {
  topQueryConfig[value] = StringParam;
}

class Top extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    pathPrefix: PropTypes.string.isRequired,
    query: tapQueryPropType.isRequired,
    setQuery: PropTypes.func.isRequired,
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.loadFromServer = this.loadFromServer.bind(this);
    this.updateTapClosingState = this.updateTapClosingState.bind(this);

    this.state = {
      error: null,
      resourcesByNs: {},
      authoritiesByNs: {},
      pollingInterval: 10000,
      tapRequestInProgress: false,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  getResourcesByNs(rsp) {
    let statTables = _get(rsp, [0, "ok", "statTables"]);
    let authoritiesByNs = {};
    let resourcesByNs = _reduce(statTables, (mem, table) => {
      _each(table.podGroup.rows, row => {
        if (row.meshedPodCount === "0") {
          return;
        }

        if (!mem[row.resource.namespace]) {
          mem[row.resource.namespace] = [];
          authoritiesByNs[row.resource.namespace] = [];
        }

        switch (row.resource.type.toLowerCase()) {
          case "service":
            break;
          case "authority":
            authoritiesByNs[row.resource.namespace].push(row.resource.name);
            break;
          default:
            mem[row.resource.namespace].push(`${row.resource.type}/${row.resource.name}`);
        }
      });
      return mem;
    }, {});
    return {
      authoritiesByNs,
      resourcesByNs
    };
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
    this.setState({
      pendingRequests: true
    });

    let url = this.api.urlsForResourceNoStats("all");
    this.api.setCurrentRequests([this.api.fetchMetrics(url)]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(rsp => {
        let { resourcesByNs, authoritiesByNs } = this.getResourcesByNs(rsp);

        this.setState({
          resourcesByNs,
          authoritiesByNs,
          pendingRequests: false
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e
    });
  }

  updateQuery = query => {
    this.props.setQuery({ ...query });
  }

  handleTapStart = () => {
    this.setState({
      tapRequestInProgress: true
    });
  }

  handleTapStop = () => {
    this.setState({
      tapRequestInProgress: false,
      tapIsClosing: true
    });
  }

  handleTapClear = () => {
    this.props.setQuery({ ...emptyTapQuery() });
    this.setState({
      error: null,
    });
  }

  updateTapClosingState() {
    this.setState({
      tapRequestInProgress: false,
      tapIsClosing: false
    });
  }

  render() {
    let queryValues = _merge(emptyTapQuery(), this.props.query); // To avoid input issues with undefined values

    return (
      <div>
        {!this.state.error ? null :
        <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />}
        <TapQueryForm
          enableAdvancedForm={false}
          cmdName="top"
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          handleTapClear={this.handleTapClear}
          resourcesByNs={this.state.resourcesByNs}
          authoritiesByNs={this.state.authoritiesByNs}
          tapRequestInProgress={this.state.tapRequestInProgress}
          tapIsClosing={this.state.tapIsClosing}
          updateQuery={this.updateQuery}
          query={queryValues} />

        <TopModule
          pathPrefix={this.props.pathPrefix}
          query={this.props.query}
          startTap={this.state.tapRequestInProgress}
          updateTapClosingState={this.updateTapClosingState} />
      </div>
    );
  }
}

export default withQueryParams(topQueryConfig, withContext(Top));
