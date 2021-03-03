import { StringParam, withQueryParams } from 'use-query-params';
import { WS_ABNORMAL_CLOSURE, WS_NORMAL_CLOSURE, WS_POLICY_VIOLATION, emptyTapQuery, processTapEvent, setMaxRps, wsCloseCodes } from './util/TapUtils.jsx';
import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import TapEnabledWarning from './TapEnabledWarning.jsx';
import TapEventTable from './TapEventTable.jsx';
import TapQueryForm from './TapQueryForm.jsx';
import _cloneDeep from 'lodash/cloneDeep';
import _each from 'lodash/each';
import _isNil from 'lodash/isNil';
import _orderBy from 'lodash/orderBy';
import _size from 'lodash/size';
import _throttle from 'lodash/throttle';
import _values from 'lodash/values';
import { groupResourcesByNs } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const urlPropsQueryConfig = {
  autostart: StringParam,
};

class Tap extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;
    this.tapResultsById = {};
    this.throttledWebsocketRecvHandler = _throttle(this.updateTapResults, 500);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      tapResultsById: this.tapResultsById,
      error: null,
      resourcesByNs: {},
      authoritiesByNs: {},
      query: {
        resource: '',
        namespace: '',
        toResource: '',
        toNamespace: '',
        method: '',
        path: '',
        scheme: '',
        authority: '',
        maxRps: '',
      },
      maxLinesToDisplay: 40,
      tapRequestInProgress: false,
      tapIsClosing: false,
      pollingInterval: 10000,
      pendingRequests: false,
      showTapEnabledWarning: false,
    };
  }

  componentDidMount() {
    const { autostart } = this.props;
    this._isMounted = true; // https://reactjs.org/blog/2015/12/16/ismounted-antipattern.html
    this.startServerPolling();
    if (autostart === 'true') {
      this.startTapStreaming();
    }
  }

  componentDidUpdate(prevProps) {
    const { isPageVisible, autostart } = this.props;
    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => {
        this.startServerPolling();
        if (autostart === 'true') {
          this.startTapStreaming();
        }
      },
      onHidden: () => {
        if (this.ws) {
          this.ws.close(1000);
        }
        this.throttledWebsocketRecvHandler.cancel();
        this.stopServerPolling();
      },
    });
  }

  componentWillUnmount() {
    this._isMounted = false;
    if (this.ws) {
      this.ws.close(1000);
    }
    this.throttledWebsocketRecvHandler.cancel();
    this.stopServerPolling();
  }

  onWebsocketOpen = () => {
    const { query } = this.state;
    const tapQuery = _cloneDeep(query);
    setMaxRps(tapQuery);

    this.ws.send(JSON.stringify({
      id: 'tap-web',
      ...tapQuery,
      extract: true,
    }));
    this.setState({
      error: null,
    });
  }

  onWebsocketRecv = e => {
    this.indexTapResult(e.data);
    this.throttledWebsocketRecvHandler();
  }

  onWebsocketClose = e => {
    this.stopTapStreaming();
    /* We ignore any abnormal closure since it doesn't matter as long as
    the connection to the websocket is closed. This is also a workaround
    where Chrome browsers incorrectly displays a 1006 close code
    https://github.com/linkerd/linkerd2/issues/1630
    */
    if (e.code !== WS_NORMAL_CLOSURE && e.code !== WS_ABNORMAL_CLOSURE && this._isMounted) {
      if (e.code === WS_POLICY_VIOLATION) {
        this.setState({
          error: {
            error: e.reason,
          },
        });
      } else if (e.reason.includes('no pods to tap')) {
        this.setState({
          showTapEnabledWarning: true,
        });
      } else {
        this.setState({
          error: {
            error: `Websocket close error [${e.code}: ${wsCloseCodes[e.code]}] ${e.reason ? ':' : ''} ${e.reason}`,
          },
        });
      }
    }
  }

  onWebsocketError = e => {
    this.setState({
      error: { error: `Websocket error: ${e.message}` },
    });

    this.stopTapStreaming();
  }

  // keep an index of tap request rows by id. this allows us to collate
  // requestInit/responseInit/responseEnd into one single table row,
  // as opposed to three separate rows as in the CLI
  indexTapResult = data => {
    const { maxLinesToDisplay } = this.state;
    const resultIndex = this.tapResultsById;
    const d = processTapEvent(data);

    if (_isNil(resultIndex[d.id])) {
      // don't let tapResultsById grow unbounded
      if (_size(resultIndex) > maxLinesToDisplay) {
        this.deleteOldestTapResult(resultIndex);
      }

      resultIndex[d.id] = {};
    }
    resultIndex[d.id][d.eventType] = d;
    // assumption: requests of a given id all share the same high level metadata
    resultIndex[d.id].base = d;
    resultIndex[d.id].key = d.id;
    resultIndex[d.id].lastUpdated = Date.now();
  }

  updateTapResults = () => {
    this.setState({
      tapResultsById: this.tapResultsById,
    });
  }

  deleteOldestTapResult = resultIndex => {
    let oldest = Date.now();
    let oldestId = '';

    _each(resultIndex, (res, id) => {
      if (res.lastUpdated < oldest) {
        oldest = res.lastUpdated;
        oldestId = id;
      }
    });

    delete resultIndex[oldestId];
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

  startTapStreaming() {
    const { pathPrefix } = this.props;
    this.tapResultsById = {};

    this.setState({
      tapRequestInProgress: true,
      tapResultsById: this.tapResultsById,
    });

    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const tapWebSocket = `${protocol}://${window.location.host}${pathPrefix}/api/tap`;

    this.ws = new WebSocket(tapWebSocket);
    this.ws.onmessage = this.onWebsocketRecv;
    this.ws.onclose = this.onWebsocketClose;
    this.ws.onopen = this.onWebsocketOpen;
    this.ws.onerror = this.onWebsocketError;
  }

  stopTapStreaming() {
    if (!this._isMounted) {
      return;
    }

    this.setState({
      tapRequestInProgress: false,
      tapIsClosing: false,
    });
  }

  handleTapStart = e => {
    e.preventDefault();
    this.startTapStreaming();
  }

  handleTapStop = () => {
    this.ws.close(1000);
    this.setState({ tapIsClosing: true });
  }

  handleTapClear = () => {
    this.resetTapResults();
  }

  resetTapResults = () => {
    this.tapResultsById = {};
    this.setState({
      tapResultsById: {},
      query: emptyTapQuery(),
      showTapEnabledWarning: false,
    });
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
    const authorityUrl = this.api.urlsForResource('authority');
    this.api.setCurrentRequests([this.api.fetchMetrics(url), this.api.fetchMetrics(authorityUrl)]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(rsp => {
        const { resourcesByNs } = groupResourcesByNs(rsp[0]);
        const { authoritiesByNs } = groupResourcesByNs(rsp[1]);
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
      showTapEnabledWarning: false,
    });
  }

  render() {
    const { tapResultsById, tapRequestInProgress, tapIsClosing, resourcesByNs, authoritiesByNs, query, showTapEnabledWarning, error } = this.state;
    const tableRows = _orderBy(_values(tapResultsById), r => r.lastUpdated, 'desc');

    return (
      <div>
        {!error ? null :
        <ErrorBanner message={error} onHideMessage={() => this.setState({ error: null })} />}

        <TapQueryForm
          cmdName="tap"
          tapRequestInProgress={tapRequestInProgress}
          tapIsClosing={tapIsClosing}
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          handleTapClear={this.handleTapClear}
          resourcesByNs={resourcesByNs}
          authoritiesByNs={authoritiesByNs}
          updateQuery={this.updateQuery}
          currentQuery={query} />
        {showTapEnabledWarning &&
          <TapEnabledWarning
            resource={query.resource}
            namespace={query.namespace}
            cardComponent />
        }
        {!showTapEnabledWarning &&
          <TapEventTable
            resource={query.resource}
            tableRows={tableRows} />
        }
      </div>
    );
  }
}

Tap.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  autostart: PropTypes.string,
  isPageVisible: PropTypes.bool.isRequired,
  pathPrefix: PropTypes.string.isRequired,
};

Tap.defaultProps = {
  autostart: '',
};

export default withPageVisibility(withQueryParams(urlPropsQueryConfig, withContext(Tap)));
