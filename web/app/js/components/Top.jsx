import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TapEnabledWarning from './TapEnabledWarning.jsx';
import TapQueryForm from './TapQueryForm.jsx';
import TopModule from './TopModule.jsx';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _reduce from 'lodash/reduce';
import { emptyTapQuery } from './util/TapUtils.jsx';
import { withContext } from './util/AppContext.jsx';

class Top extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.loadFromServer = this.loadFromServer.bind(this);
    this.updateTapClosingState = this.updateTapClosingState.bind(this);

    this.state = {
      error: null,
      resourcesByNs: {},
      authoritiesByNs: {},
      query: emptyTapQuery(),
      pollingInterval: 10000,
      tapRequestInProgress: false,
      pendingRequests: false,
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentDidUpdate(prevProps) {
    const { isPageVisible } = this.props;
    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  getResourcesByNs = rsp => {
    const statTables = _get(rsp, [0, 'ok', 'statTables']);
    const authoritiesByNs = {};
    const resourcesByNs = _reduce(statTables, (mem, table) => {
      _each(table.podGroup.rows, row => {
        if (row.meshedPodCount === '0') {
          return;
        }

        if (!mem[row.resource.namespace]) {
          mem[row.resource.namespace] = [];
          authoritiesByNs[row.resource.namespace] = [];
        }

        switch (row.resource.type.toLowerCase()) {
          case 'service':
            break;
          case 'authority':
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
      resourcesByNs,
    };
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

  loadFromServer() {
    const { pendingRequests } = this.state;

    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({
      pendingRequests: true,
    });

    const url = this.api.urlsForResourceNoStats('all');
    this.api.setCurrentRequests([this.api.fetchMetrics(url)]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(rsp => {
        const { resourcesByNs, authoritiesByNs } = this.getResourcesByNs(rsp);

        this.setState({
          resourcesByNs,
          authoritiesByNs,
          pendingRequests: false,
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
      error: e,
    });
  }

  updateQuery = query => {
    this.setState({
      query,
    });
  }

  handleTapStart = () => {
    this.setState({
      tapRequestInProgress: true,
    });
  }

  handleTapStop = () => {
    this.setState({
      tapRequestInProgress: false,
      tapIsClosing: true,
    });
  }

  handleTapClear = () => {
    this.setState({
      error: null,
      query: emptyTapQuery(),
    });
  }

  updateTapClosingState() {
    this.setState({
      tapRequestInProgress: false,
      tapIsClosing: false,
    });
  }

  render() {
    const { query, resourcesByNs, authoritiesByNs, tapRequestInProgress, tapIsClosing, error } = this.state;
    const { pathPrefix } = this.props;

    return (
      <div>
        {!error ? null :
        <ErrorBanner message={error} onHideMessage={() => this.setState({ error: null })} />}
        <TapQueryForm
          enableAdvancedForm={false}
          cmdName="top"
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          handleTapClear={this.handleTapClear}
          resourcesByNs={resourcesByNs}
          authoritiesByNs={authoritiesByNs}
          tapRequestInProgress={tapRequestInProgress}
          tapIsClosing={tapIsClosing}
          updateQuery={this.updateQuery}
          currentQuery={query} />
        <TopModule
          pathPrefix={pathPrefix}
          query={query}
          startTap={tapRequestInProgress}
          tapEnabledWarningComponent={<TapEnabledWarning
            resource={query.resource}
            namespace={query.namespace}
            cardComponent />}
          updateTapClosingState={this.updateTapClosingState} />
      </div>
    );
  }
}

Top.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  pathPrefix: PropTypes.string.isRequired,
};

export default withPageVisibility(withContext(Top));
