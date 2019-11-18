import { UrlQueryParamTypes, addUrlProps } from 'react-url-query';
import { WS_ABNORMAL_CLOSURE, WS_NORMAL_CLOSURE, WS_POLICY_VIOLATION, emptyTapQuery, processTapEvent, setMaxRps, wsCloseCodes } from './util/TapUtils.jsx';

import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
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
  autostart: { type: UrlQueryParamTypes.string }
};

class Tap extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    autostart: PropTypes.string,
    pathPrefix: PropTypes.string.isRequired
  }

  static defaultProps = {
    autostart: ""
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.tapResultsById = {};
    this.throttledWebsocketRecvHandler = _throttle(this.updateTapResults, 500);
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      tapResultsById: this.tapResultsById,
      error: null,
      resourcesByNs: {},
      authoritiesByNs: {},
      query: {
        resource: "",
        namespace: "",
        toResource: "",
        toNamespace: "",
        method: "",
        path: "",
        scheme: "",
        authority: "",
        maxRps: ""
      },
      maxLinesToDisplay: 40,
      tapRequestInProgress: false,
      tapIsClosing: false,
      pollingInterval: 10000,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this._isMounted = true; // https://reactjs.org/blog/2015/12/16/ismounted-antipattern.html
    this.startServerPolling();
    if (this.props.autostart === "true") {
      this.startTapStreaming();
    }
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
    let query = _cloneDeep(this.state.query);
    setMaxRps(query);

    this.ws.send(JSON.stringify({
      id: "tap-web",
      ...query,
      extract: true,
    }));
    this.setState({
      error: null
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
            error: e.reason
          }
        });
      } else {
        this.setState({
          error: {
            error: `Websocket close error [${e.code}: ${wsCloseCodes[e.code]}] ${e.reason ? ":" : ""} ${e.reason}`
          }
        });
      }
    }
  }

  onWebsocketError = e => {
    this.setState({
      error: { error: `Websocket error: ${e.message}` }
    });

    this.stopTapStreaming();
  }

  indexTapResult = data => {
    // keep an index of tap request rows by id. this allows us to collate
    // requestInit/responseInit/responseEnd into one single table row,
    // as opposed to three separate rows as in the CLI
    let resultIndex = this.tapResultsById;
    let d = processTapEvent(data);

    if (_isNil(resultIndex[d.id])) {
      // don't let tapResultsById grow unbounded
      if (_size(resultIndex) > this.state.maxLinesToDisplay) {
        this.deleteOldestTapResult(resultIndex);
      }

      resultIndex[d.id] = {};
    }
    resultIndex[d.id][d.eventType] = d;
    // assumption: requests of a given id all share the same high level metadata
    resultIndex[d.id]["base"] = d;
    resultIndex[d.id].key = d.id;
    resultIndex[d.id].lastUpdated = Date.now();
  }

  updateTapResults = () => {
    this.setState({
      tapResultsById: this.tapResultsById
    });
  }

  deleteOldestTapResult = resultIndex => {
    let oldest = Date.now();
    let oldestId = "";

    _each(resultIndex, (res, id) => {
      if (res.lastUpdated < oldest) {
        oldest = res.lastUpdated;
        oldestId = id;
      }
    });

    delete resultIndex[oldestId];
  }

  startServerPolling() {
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopServerPolling() {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  startTapStreaming() {
    this.tapResultsById = {};

    this.setState({
      tapRequestInProgress: true,
      tapResultsById: this.tapResultsById
    });

    let protocol = window.location.protocol === "https:" ? "wss" : "ws";
    let tapWebSocket = `${protocol}://${window.location.host}${this.props.pathPrefix}/api/tap`;

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
      tapIsClosing: false
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
      query: emptyTapQuery()
    });
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
      .then(([rsp]) => {
        let { resourcesByNs, authoritiesByNs } = groupResourcesByNs(rsp);

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
    this.setState({
      query
    });
  }

  render() {
    let tableRows = _orderBy(_values(this.state.tapResultsById), r => r.lastUpdated, "desc");

    return (
      <div>
        {!this.state.error ? null :
        <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />}

        <TapQueryForm
          cmdName="tap"
          tapRequestInProgress={this.state.tapRequestInProgress}
          tapIsClosing={this.state.tapIsClosing}
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          handleTapClear={this.handleTapClear}
          resourcesByNs={this.state.resourcesByNs}
          authoritiesByNs={this.state.authoritiesByNs}
          updateQuery={this.updateQuery}
          query={this.state.query} />

        <TapEventTable
          resource={this.state.query.resource}
          tableRows={tableRows} />
      </div>
    );
  }
}

export default addUrlProps({ urlPropsQueryConfig })(withContext(Tap));
