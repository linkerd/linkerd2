import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import PageHeader from './PageHeader.jsx';
import PropTypes from 'prop-types';
import { publicAddressToString } from './util/Utils.js';
import React from 'react';
import TapEventTable from './TapEventTable.jsx';
import TapQueryForm from './TapQueryForm.jsx';
import { withContext } from './util/AppContext.jsx';
import './../../css/tap.css';

const httpMethods = ["GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH"];
const defaultMaxRps = 1.0;
const maxNumFilterOptions = 12;
class Tap extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    pathPrefix: PropTypes.string.isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.loadFromServer = this.loadFromServer.bind(this);

    this.state = {
      tapResultsById: {},
      tapResultFilterOptions: this.getInitialTapFilterOptions(),
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
        maxRps: defaultMaxRps
      },
      maxLinesToDisplay: 40,
      awaitingWebSocketConnection: false,
      tapRequestInProgress: false,
      pollingInterval: 10000,
      pendingRequests: false
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentWillUnmount() {
    if (this.ws) {
      this.ws.close(1000);
    }
    this.stopTapStreaming();
    this.stopServerPolling();
  }

  onWebsocketOpen = () => {
    let query = this.state.query;
    query.maxRps = parseFloat(query.maxRps) || defaultMaxRps;

    this.ws.send(JSON.stringify({
      id: "tap-web",
      ...query
    }));
    this.setState({
      awaitingWebSocketConnection: false,
      error: null
    });
  }

  onWebsocketRecv = e => {
    this.indexTapResult(e.data);
  }

  onWebsocketClose = e => {
    this.stopTapStreaming();

    if (!e.wasClean) {
      this.setState({
        error: {
          error: `Websocket [${e.code}] ${e.reason}`
        }
      });
    }
  }

  onWebsocketError = e => {
    this.setState({
      error: { error: e.message }
    });

    this.stopTapStreaming();
  }

  getInitialTapFilterOptions() {
    return {
      source: {},
      destination: {},
      path: {},
      authority: {},
      scheme: {},
      httpStatus: {},
      tls: {},
      httpMethod: httpMethods
    };
  }

  getResourcesByNs(rsp) {
    let statTables = _.get(rsp, [0, "ok", "statTables"]);
    let authoritiesByNs = {};
    let resourcesByNs = _.reduce(statTables, (mem, table) => {
      _.map(table.podGroup.rows, row => {
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

  parseTapResult = data => {
    let d = JSON.parse(data);
    let filters = this.state.tapResultFilterOptions;

    // keep track of unique values we encounter, to populate the table filters
    let addFilter = this.genFilterAdder(filters, Date.now());
    d.source.str = publicAddressToString(_.get(d, "source.ip.ipv4"), d.source.port);
    addFilter("source", d.source.str);
    d.destination.str = publicAddressToString(_.get(d, "destination.ip.ipv4"), d.destination.port);
    addFilter("destination", d.destination.str);

    switch (d.proxyDirection) {
      case "INBOUND":
        d.tls = _.get(d, "sourceMeta.labels.tls", "");
        addFilter("tls", d.tls);
        break;
      case "OUTBOUND":
        d.tls = _.get(d, "destinationMeta.labels.tls", "");
        addFilter("tls", d.tls);
        break;
      default:
        // too old for TLS
    }

    if (_.isNil(d.http)) {
      this.setState({ error: "Undefined request type"});
    } else {
      if (!_.isNil(d.http.requestInit)) {
        d.eventType = "req";
        d.id = `${_.get(d, "http.requestInit.id.base")}:${_.get(d, "http.requestInit.id.stream")} `;

        addFilter("authority", d.http.requestInit.authority);
        addFilter("path", d.http.requestInit.path);
        addFilter("scheme", _.get(d, "http.requestInit.scheme.registered"));
      } else if (!_.isNil(d.http.responseInit)) {
        d.eventType = "rsp";
        d.id = `${_.get(d, "http.responseInit.id.base")}:${_.get(d, "http.responseInit.id.stream")} `;

        addFilter("httpStatus", _.get(d, "http.responseInit.httpStatus"));
      } else if (!_.isNil(d.http.responseEnd)) {
        d.eventType = "end";
        d.id = `${_.get(d, "http.responseEnd.id.base")}:${_.get(d, "http.responseEnd.id.stream")} `;
      }
    }

    return {
      tapResult: d,
      updatedFilters: filters
    };
  }

  genFilterAdder(filterOptions, now) {
    return (filterName, filterValue) => {
      filterOptions[filterName][filterValue] = now;

      if (_.size(filterOptions[filterName]) > maxNumFilterOptions) {
        // reevaluate this if table updating gets too slow
        let oldest = Date.now();
        let oldestOption = "";
        _.each(filterOptions[filterName], (timestamp, value) => {
          if (timestamp < oldest) {
            oldest = timestamp;
            oldestOption = value;
          }
        });

        delete filterOptions[filterName][oldestOption];
      }
    };
  }

  indexTapResult = data => {
    // keep an index of tap request rows by id. this allows us to collate
    // requestInit/responseInit/responseEnd into one single table row,
    // as opposed to three separate rows as in the CLI
    let resultIndex = this.state.tapResultsById;
    let parsedResults = this.parseTapResult(data);
    let d = parsedResults.tapResult;

    if (_.isNil(resultIndex[d.id])) {
      // don't let tapResultsById grow unbounded
      if (_.size(resultIndex) > this.state.maxLinesToDisplay) {
        this.deleteOldestTapResult(resultIndex);
      }

      resultIndex[d.id] = {};
    }
    resultIndex[d.id][d.eventType] = d;
    // assumption: requests of a given id all share the same high level metadata
    resultIndex[d.id]["base"] = d;
    resultIndex[d.id].lastUpdated = Date.now();

    this.setState({
      tapResultsById: resultIndex,
      tapResultFilterOptions: parsedResults.updatedFilters
    });
  }

  deleteOldestTapResult = resultIndex => {
    let oldest = Date.now();
    let oldestId = "";

    _.each(resultIndex, (res, id) => {
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

  startTapSteaming() {
    this.setState({
      awaitingWebSocketConnection: true,
      tapRequestInProgress: true,
      tapResultsById: {},
      tapResultFilterOptions: this.getInitialTapFilterOptions()
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
    this.setState({
      tapRequestInProgress: false,
      awaitingWebSocketConnection: false
    });
  }

  handleTapStart = e => {
    e.preventDefault();
    this.startTapSteaming();
  }

  handleTapStop = () => {
    this.ws.close(1000);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({
      pendingRequests: true
    });

    let url = "/api/tps-reports?resource_type=all&all_namespaces=true";
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

  renderCurrentQuery = () => {
    let emptyVals =  _.countBy(_.values(this.state.query), v => _.isEmpty(v));

    return (
      <div className="tap-query">
        <code>
          { !emptyVals.false || emptyVals.false === 1 ? null :
          <div>
            Current query:
            {
              _.map(this.state.query, (query, queryName) => {
                if (query === "") {
                  return null;
                }
                return <div key={queryName}>{queryName}: {query}</div>;
              })
            }
          </div>
          }
        </code>
      </div>
    );
  }

  render() {
    let tableRows = _(this.state.tapResultsById)
      .values().sortBy('lastUpdated').reverse().value();

    return (
      <div>
        {!this.state.error ? null :
        <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />}

        <PageHeader header="Tap" />
        <TapQueryForm
          tapRequestInProgress={this.state.tapRequestInProgress}
          awaitingWebSocketConnection={this.state.awaitingWebSocketConnection}
          handleTapStart={this.handleTapStart}
          handleTapStop={this.handleTapStop}
          resourcesByNs={this.state.resourcesByNs}
          authoritiesByNs={this.state.authoritiesByNs}
          query={this.state.query} />
        {this.renderCurrentQuery()}

        <TapEventTable
          tableRows={tableRows}
          filterOptions={this.state.tapResultFilterOptions} />
      </div>
    );
  }
}

export default withContext(Tap);
