import _ from 'lodash';
import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import ExpansionPanel from '@material-ui/core/ExpansionPanel';
import ExpansionPanelDetails from '@material-ui/core/ExpansionPanelDetails';
import ExpansionPanelSummary from '@material-ui/core/ExpansionPanelSummary';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import Grid from '@material-ui/core/Grid';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import PropTypes from 'prop-types';
import React from 'react';
import Select from '@material-ui/core/Select';
import TextField from '@material-ui/core/TextField';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';
import { addUrlProps, UrlQueryParamTypes } from 'react-url-query';
import {
  defaultMaxRps,
  emptyTapQuery,
  httpMethods,
  tapQueryProps,
  tapQueryPropType
} from './util/TapUtils.jsx';

// you can also tap resources to tap all pods in the resource
const resourceTypes = [
  "deployment",
  "daemonset",
  "pod",
  "replicationcontroller",
  "statefulset"
];

const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _.uniq(_.flatten(_.values(resourcesByNs)));
};

const urlPropsQueryConfig = _.mapValues(tapQueryProps, () => {
  return { type: UrlQueryParamTypes.string };
});

const styles = theme => ({
  root: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  formControl: {
    margin: theme.spacing.unit,
    minWidth: 200,
  },
  selectEmpty: {
    'margin-top': '32px'
  },
  card: {
    maxWidth: "100%",
  },
  actions: {
    display: 'flex',
    'padding-left': '32px'
  },
  expand: {
    transform: 'rotate(0deg)',
    transition: theme.transitions.create('transform', {
      duration: theme.transitions.duration.shortest,
    }),
    marginLeft: 'auto',
    [theme.breakpoints.up('sm')]: {
      marginRight: -8,
    },
  },
  expandOpen: {
    transform: 'rotate(180deg)',
  }
});


class TapQueryForm extends React.Component {
  static propTypes = {
    classes: PropTypes.shape({}).isRequired,
    enableAdvancedForm: PropTypes.bool,
    handleTapClear: PropTypes.func,
    handleTapStart: PropTypes.func.isRequired,
    handleTapStop: PropTypes.func.isRequired,
    query: tapQueryPropType.isRequired,
    tapIsClosing: PropTypes.bool,
    tapRequestInProgress: PropTypes.bool.isRequired,
    updateQuery: PropTypes.func.isRequired
  }

  static defaultProps = {
    enableAdvancedForm: true,
    handleTapClear: _.noop,
    tapIsClosing: false
  }

  constructor(props) {
    super(props);

    let query = _.merge({}, props.query, _.pick(this.props, _.keys(tapQueryProps)));
    props.updateQuery(query);

    let advancedFormExpanded = _.some(
      _.omit(query, ['namespace', 'resource']),
      v => !_.isEmpty(v));

    this.state = {
      query,
      advancedFormExpanded,
      authoritiesByNs: {},
      resourcesByNs: {},
      autocomplete: {
        namespace: [],
        resource: [],
        toNamespace: [],
        toResource: [],
        authority: []
      },
    };
  }

  static getDerivedStateFromProps(props, state) {
    if (!_.isEqual(props.resourcesByNs, state.resourcesByNs)) {
      let resourcesByNs = props.resourcesByNs;
      let authoritiesByNs = props.authoritiesByNs;
      let namespaces = _.sortBy(_.keys(resourcesByNs));
      let resourceNames  = getResourceList(resourcesByNs, state.query.namespace);
      let toResourceNames = getResourceList(resourcesByNs, state.query.toNamespace);
      let authorities = getResourceList(authoritiesByNs, state.query.namespace);

      return _.merge(state, {
        resourcesByNs,
        authoritiesByNs,
        autocomplete: {
          namespace: namespaces,
          resource: resourceNames,
          toNamespace: namespaces,
          toResource: toResourceNames,
          authority: authorities
        }
      });
    } else {
      return null;
    }
  }

  handleFormChange = (name, scopeResource) => {
    let state = {
      query: this.state.query,
      autocomplete: this.state.autocomplete
    };

    let shouldScopeAuthority = name === "namespace";

    return event => {
      let formVal = event.target.value;
      state.query[name] = formVal;
      this.handleUrlUpdate(name, formVal);

      if (!_.isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = this.state.resourcesByNs[formVal];
        state.query[scopeResource] = `namespace/${formVal}`;
        this.handleUrlUpdate(scopeResource, `namespace/${formVal}`);
      }

      if (shouldScopeAuthority) {
        state.autocomplete.authority = this.state.authoritiesByNs[formVal];
      }

      this.setState(state);
      this.props.updateQuery(state.query);
    };
  }

  // Each time state.query is updated, this method calls the equivalent
  // onChange method to reflect the update in url query params. These onChange
  // methods are automatically added to props by react-url-query.
  handleUrlUpdate = (name, formVal) => {
    this.props[`onChange${_.upperFirst(name)}`](formVal);
  }

  handleFormEvent = name => {
    let state = {
      query: this.state.query
    };

    return event => {
      state.query[name] = event.target.value;
      this.handleUrlUpdate(name, event.target.value);
      this.setState(state);
      this.props.updateQuery(state.query);
    };
  }

  handleAdvancedFormExpandClick = () => {
    this.setState(state => ({advancedFormExpanded: !state.advancedFormExpanded}));
  }

  autoCompleteData = name => {
    return _(this.state.autocomplete[name])
      .filter(d => d.indexOf(this.state.query[name]) !== -1)
      .uniq()
      .sortBy()
      .value();
  }

  resetTapForm = () => {
    this.setState({
      query: emptyTapQuery()
    });

    _.each(this.state.query, (_val, name) => {
      this.handleUrlUpdate(name, null);
    });

    this.props.updateQuery(emptyTapQuery(), true);
    this.props.handleTapClear();
  }

  renderResourceSelect = (resourceKey, namespaceKey) => {
    let selectedNs = this.state.query[namespaceKey];
    let nsEmpty = _.isNil(selectedNs) || _.isEmpty(selectedNs);
    let { classes } = this.props;

    let resourceOptions = _.concat(
      resourceTypes,
      this.state.autocomplete[resourceKey] || [],
      nsEmpty ? [] : [`namespace/${selectedNs}`]
    );

    return (
      <React.Fragment>
        <InputLabel htmlFor={resourceKey}>{_.startCase(resourceKey)}</InputLabel>
        <Select
          value={nsEmpty ? _.startCase(resourceKey) : this.state.query[resourceKey]}
          onChange={this.handleFormChange(resourceKey)}
          inputProps={{ name: resourceKey, id: resourceKey }}
          className={classes.selectEmpty}>
          {
            _.map(_.sortBy(resourceOptions), resource => (
              <MenuItem key={`${namespaceKey}-${resourceKey}-${resource}`} value={resource}>{resource}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderNamespaceSelect = (title, namespaceKey, resourceKey) => {
    let { classes } = this.props;
    return (
      <React.Fragment>
        <InputLabel htmlFor={namespaceKey}>{title}</InputLabel>
        <Select
          value={this.state.query[namespaceKey]}
          onChange={this.handleFormChange(namespaceKey, resourceKey)}
          inputProps={{ name: namespaceKey, id: namespaceKey }}
          className={classes.selectEmpty}>
          {
            _.map(this.state.autocomplete[namespaceKey], (n, i) => (
              <MenuItem key={`ns-dr-${i}`} value={n}>{n}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderTapButton = (tapInProgress, tapIsClosing) => {
    if (tapIsClosing) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" disabled={true}>Stop</Button>);
    } else if (tapInProgress) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" onClick={this.props.handleTapStop}>Stop</Button>);
    } else {
      return (
        <Button
          color="primary"
          variant="outlined"
          className="tap-ctrl tap-start"
          disabled={!this.state.query.namespace || !this.state.query.resource}
          onClick={this.props.handleTapStart}>
          Start
        </Button>);
    }
  }

  renderTextInput = (title, key, helperText) => {
    let { classes } = this.props;
    return (
      <TextField
        id={key}
        label={title}
        className={classes.textField}
        value={this.state.query[key]}
        onChange={this.handleFormEvent(key)}
        helperText={helperText}
        margin="normal" />
    );
  }

  renderAdvancedTapFormContent() {
    const { classes } = this.props;

    return (
      <Grid container spacing={24}>
        <Grid item xs={6} md={3}>
          <FormControl className={classes.formControl}>
            {this.renderNamespaceSelect("To Namespace", "toNamespace", "toResource")}
          </FormControl>
        </Grid>

        <Grid item xs={6} md={3}>
          <FormControl className={classes.formControl} disabled={_.isEmpty(this.state.query.toNamespace)}>
            {this.renderResourceSelect("toResource", "toNamespace")}
          </FormControl>
        </Grid>

        <Grid container spacing={24}>
          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="authority">Authority</InputLabel>
              <Select
                value={this.state.query.authority}
                onChange={this.handleFormChange("authority")}
                inputProps={{ name: 'authority', id: 'authority' }}
                className={classes.selectEmpty}>
                {
                _.map(this.state.autocomplete.authority, (d, i) => (
                  <MenuItem key={`authority-${i}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>Display requests with this :authority</FormHelperText>
            </FormControl>
          </Grid>

          <Grid item xs={6} md={3}>
            { this.renderTextInput("Path", "path", "Display requests with paths that start with this prefix") }
          </Grid>
        </Grid>

        <Grid container spacing={24}>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Scheme", "scheme", "Display requests with this scheme") }
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Max RPS", "maxRps", `Maximum requests per second to tap. Default ${defaultMaxRps}`) }
          </Grid>

          <Grid item xs={6} md={3}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="method">HTTP method</InputLabel>
              <Select
                value={this.state.query.method}
                onChange={this.handleFormChange("method")}
                inputProps={{ name: 'method', id: 'method' }}
                className={classes.selectEmpty}>
                {
                _.map(httpMethods, d => (
                  <MenuItem key={`method-${d}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>Display requests with this HTTP method</FormHelperText>
            </FormControl>
          </Grid>
        </Grid>
      </Grid>
    );
  }

  renderAdvancedTapForm() {
    return (
      <ExpansionPanel expanded={this.state.advancedFormExpanded} onChange={this.handleAdvancedFormExpandClick}>
        <ExpansionPanelSummary expandIcon={<ExpandMoreIcon />}>
          <Typography paragraph>{this.state.advancedFormExpanded ? "Hide filters" : "Show more filters"}</Typography>
        </ExpansionPanelSummary>

        <ExpansionPanelDetails>
          {this.renderAdvancedTapFormContent()}
        </ExpansionPanelDetails>
      </ExpansionPanel>
    );
  }

  render() {
    const { classes } = this.props;

    return (
      <Card className={classes.card}>
        <CardContent>
          <Grid container spacing={24}>
            <Grid item xs={6} md={3}>
              <FormControl className={classes.formControl}>
                {this.renderNamespaceSelect("Namespace", "namespace", "resource")}
              </FormControl>
            </Grid>

            <Grid item xs={6} md={3}>
              <FormControl className={classes.formControl} disabled={_.isEmpty(this.state.query.namespace)}>
                {this.renderResourceSelect("resource", "namespace")}
              </FormControl>
            </Grid>

            <Grid item xs={4} md={1}>
              { this.renderTapButton(this.props.tapRequestInProgress, this.props.tapIsClosing) }
            </Grid>

            <Grid item xs={4} md={1}>
              <Button onClick={this.resetTapForm} disabled={this.props.tapRequestInProgress}>Reset</Button>
            </Grid>
          </Grid>
        </CardContent>

        { !this.props.enableAdvancedForm ? null :  this.renderAdvancedTapForm() }

      </Card>
    );
  }
}

export default addUrlProps({ urlPropsQueryConfig })(withStyles(styles)(TapQueryForm));
